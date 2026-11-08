#!/usr/bin/env python3
"""Measure native default/minimum profiles against local dummy producers and an unavailable backend.

This is a host smoke test, not embedded hardware certification or producer ABI evidence.
RSS includes runtime/libraries; MAX_BUFFER_BYTES only charges retained telemetry.
"""
import argparse
import base64
from collections import deque
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import signal
import socket
import struct
import subprocess
import tempfile
import threading
import time
import shutil

ROOT = Path(__file__).resolve().parent.parent
EUI = 0x0102030405060708


def exact(conn, size):
    result = b''
    while len(result) < size:
        part = conn.recv(size - len(result))
        if not part:
            raise EOFError
        result += part
    return result


class LNS(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def do_GET(self):
        key = self.headers['Sec-WebSocket-Key']
        accept = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
        self.send_response(101)
        self.send_header('Upgrade', 'websocket')
        self.send_header('Connection', 'Upgrade')
        self.send_header('Sec-WebSocket-Accept', accept)
        self.end_headers()
        try:
            while True:
                first, second = exact(self.connection, 2)
                size = second & 127
                if size == 126:
                    size = struct.unpack('!H', exact(self.connection, 2))[0]
                elif size == 127:
                    size = struct.unpack('!Q', exact(self.connection, 8))[0]
                mask = exact(self.connection, 4) if second & 128 else None
                payload = exact(self.connection, size)
                if mask:
                    payload = bytes(v ^ mask[i % 4] for i, v in enumerate(payload))
                if first & 15 == 8:
                    break
                if first & 15 == 9:
                    self.connection.sendall(bytes([0x8a, len(payload)]) + payload)
        except (OSError, EOFError):
            pass
        self.close_connection = True


def frame(payload):
    mask = b'\x01\x02\x03\x04'
    prefix = bytes([0x81, 0x80 | len(payload)]) if len(payload) < 126 else bytes([0x81, 0xfe]) + struct.pack('!H', len(payload))
    return prefix + mask + bytes(v ^ mask[i % 4] for i, v in enumerate(payload))


def connect_gateway(port):
    until = time.monotonic() + 5
    while True:
        try:
            conn = socket.create_connection(('127.0.0.1', port), timeout=1)
            break
        except OSError:
            if time.monotonic() >= until:
                raise
            time.sleep(.02)
    request = (f'GET /?router={EUI:016x} HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: Zml4dHVyZS1zbW9rZS0wMQ==\r\n\r\n')
    conn.sendall(request.encode())
    response = b''
    while b'\r\n\r\n' not in response:
        response += exact(conn, 1)
    if b' 101 ' not in response.split(b'\r\n')[0]:
        raise RuntimeError('fixture gateway upgrade failed: ' + response.decode())
    return conn


def rss(pid):
    # ps reports KiB on both Linux and macOS.
    return int(subprocess.check_output(['ps', '-o', 'rss=', '-p', str(pid)], text=True).strip()) * 1024


def inventory(root):
    return [{'name': str(p.relative_to(root)), 'bytes': p.lstat().st_size,
             'type': 'socket' if p.is_socket() else 'file'}
            for p in sorted(root.rglob('*')) if not p.is_dir()]


def dependencies(binary):
    """Report existing non-system dynamic libraries, de-duplicated by real path."""
    found = {}
    if platform.system() == 'Darwin':
        pending = [binary]
        while pending:
            output = subprocess.check_output(['otool', '-L', str(pending.pop())], text=True)
            for line in output.splitlines()[1:]:
                name = line.strip().split(' (', 1)[0]
                path = Path(name)
                if not path.is_absolute() or name.startswith(('/usr/lib/', '/System/')) or not path.is_file():
                    continue
                path = path.resolve()
                if path not in found:
                    found[path] = path.stat().st_size
                    pending.append(path)
    elif shutil.which('ldd'):
        result = subprocess.run(['ldd', str(binary)], text=True, capture_output=True)
        for line in result.stdout.splitlines():
            for part in line.split():
                path = Path(part)
                if path.is_absolute() and path.is_file():
                    found[path.resolve()] = path.stat().st_size
    return dict(bytes=sum(found.values()), files=[dict(name=p.name, bytes=size) for p,size in sorted(found.items())],
                caveat='existing dynamic library files; excludes OS shared cache and libraries not resolvable on this host')


def run(binary, component, profile, seconds, lns_port):
    with tempfile.TemporaryDirectory(prefix='analytics-resource-') as directory:
        work = Path(directory)
        env = {k: v for k, v in os.environ.items() if k in ['PATH', 'HOME', 'TMPDIR', 'DYLD_LIBRARY_PATH', 'LD_LIBRARY_PATH']}
        env.update(CLIENT_ID='00112233445566778899aabb', CLIENT_KEY='00112233445566778899aabbccddeeff',
                   GATEWAY='aaaaaaaaaaaaaaaaaaaaaaaa', ANALYTICS_ENDPOINT='127.0.0.1:1',
                   SHUTDOWN_DEADLINE_MS='100', BATCH_SEC='1')
        if profile == 'minimum':
            sample = ROOT / ('forwarder-bs/minimum.env' if component == 'go' else 'basicstation-analytics/packaging/minimum.env')
            for line in sample.read_text().splitlines():
                if line and not line.startswith('#'):
                    k, v = line.split('=', 1)
                    env[k] = v
        env['LOG_LEVEL'] = 'info'  # bounded pipe capture for observable loss counters
        if component == 'go':
            with socket.socket() as probe:
                probe.bind(('127.0.0.1', 0))
                port = probe.getsockname()[1]
            env.update(CONNECT_URI=f'ws://127.0.0.1:{lns_port}', LISTEN_PORT=str(port))
        else:
            env['ANALYTICS_SOCK'] = str(work / 'analytics.sock')
        process = subprocess.Popen([str(binary)], cwd=work, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        lines = deque(maxlen=20)
        reader = threading.Thread(target=lambda: lines.extend(process.stdout), daemon=True)
        reader.start()
        conn = None
        try:
            if component == 'go':
                conn = connect_gateway(port)
            else:
                until = time.monotonic() + 5
                while not (work / 'analytics.sock').exists():
                    if process.poll() is not None or time.monotonic() > until:
                        raise RuntimeError('consumer did not start: ' + ''.join(lines))
                    time.sleep(.02)
                conn = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
                conn.connect(str(work / 'analytics.sock'))
                conn.send(struct.pack('=BBBBIQ8s', 0xa7, 1, 4, 0, 0x01020304, EUI, b'fixture'))
            time.sleep(.2)
            idle = rss(process.pid)
            samples = [idle]
            sent = 0
            start = time.monotonic()
            while time.monotonic() - start < seconds:
                # Distinct proprietary payloads avoid intentional packet deduplication.
                payload = b'\xe0' + sent.to_bytes(4, 'little')
                if component == 'go':
                    packet = json.dumps({'msgtype': 'propdf', 'FRMPayload': payload.hex(), 'DR': 5,
                                         'Freq': 868100000, 'upinfo': {'rctx': 0, 'xtime': sent, 'rssi': -40, 'snr': 5}}).encode()
                    conn.sendall(frame(packet))
                else:
                    prefix = struct.pack('=BBBBIqqqIhhiBBBBH', 0xa7, 1, 1, 0, 0x01020304,
                                         time.time_ns() // 1000, 0, sent, 868100000, -40, 20, 0, 5, 7, 0, 1, len(payload))
                    conn.send(prefix + payload)
                sent += 1
                if sent % 20 == 0:
                    samples.append(rss(process.pid))
                time.sleep(.005)
            during = inventory(work)
            shutdown_start = time.monotonic()
            process.send_signal(signal.SIGTERM)
            code = process.wait(timeout=2)
            elapsed = time.monotonic() - shutdown_start
            reader.join(timeout=1)
            after = inventory(work)
            final = next((line.strip() for line in reversed(lines) if 'stopped ' in line), '')
            if not final or 'retained_messages=0 ' in final or 'dropped_messages=0 ' in final:
                raise RuntimeError('fixture did not exercise retained telemetry and overflow: ' + ''.join(lines))
            if elapsed > .5 or code != 2:
                raise RuntimeError(f'shutdown deadline regression: {elapsed}s exit={code}')
            if any(item['type'] == 'file' and item['bytes'] != 0 for item in after):
                raise RuntimeError('unexpected application disk writes')
            return dict(component=component, profile=profile, duration_seconds=seconds,
                        sent=sent, idle_rss_bytes=idle, peak_sampled_rss_bytes=max(samples),
                        shutdown_seconds=round(elapsed, 4), exit_code=code,
                        during_workload= during, after_shutdown=after, final_status=final)
        finally:
            if conn:
                conn.close()
            if process.poll() is None:
                process.kill()
                process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-binary', type=Path, required=True)
    parser.add_argument('--cpp-binary', type=Path, required=True)
    parser.add_argument('--seconds', type=float, default=3)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    fixture = ThreadingHTTPServer(('127.0.0.1', 0), LNS)
    fixture.daemon_threads = True
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    try:
        binaries = {'go': args.go_binary.resolve(), 'cpp': args.cpp_binary.resolve()}
        source_commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        source_dirty = bool(subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=ROOT,text=True).strip())
        binary_info = {component: dict(bytes=binary.stat().st_size, sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                                      version=subprocess.check_output([str(binary),'--version'],text=True).strip(),
                                      dynamic_dependencies=dependencies(binary)) for component,binary in binaries.items()}
        results = [run(binary, component, profile, args.seconds, fixture.server_port)
                   for component, binary in binaries.items() for profile in ['default', 'minimum']]
        result = dict(scope='native dummy-fixture smoke; not producer ABI or target certification',
                      platform=platform.platform(), machine=platform.machine(),
                      source_commit=source_commit, source_dirty=source_dirty,
                      overrides={'BATCH_SEC': 1, 'SHUTDOWN_DEADLINE_MS': 100, 'LOG_LEVEL': 'info'},
                      caveats=['sampled RSS includes language runtime, libraries and allocator; no absolute device budget is claimed',
                               'ps sampling can miss transient peaks; socket inode sizes do not measure filesystem block allocation',
                               'default configuration writes no telemetry spool or application log; zero-byte socket lock is retained'],
                      binaries=binary_info,
                      results=results)
        args.output.write_text(json.dumps(result, indent=2) + '\n')
        print(args.output)
    finally:
        fixture.shutdown()
        fixture.server_close()


if __name__ == '__main__':
    main()
