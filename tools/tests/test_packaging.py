import importlib.util
import io
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('build_ipk',ROOT/'tools/build_ipk.py')
packager=importlib.util.module_from_spec(spec);spec.loader.exec_module(packager)

class PackagingTest(unittest.TestCase):
    def test_deterministic_private_installation(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);binary=root/'fixture'
            # Structural packaging fixture, never executed or released.
            data=bytearray(64);data[:6]=b'\x7fELF\x01\x01';struct.pack_into('<H',data,18,40);binary.write_bytes(data)
            out=root/'fixture.ipk';assets=ROOT/'basicstation-analytics/packaging/kerlink'
            packager.build(binary,assets,'0.1.0','fixture-runtime',out)
            first=out.read_bytes();packager.build(binary,assets,'0.1.0','fixture-runtime',out)
            self.assertEqual(first,out.read_bytes())
            self.assertEqual(first[:8],b'!<arch>\n')
            offset=8;members={}
            while offset<len(first):
                header=first[offset:offset+60];size=int(header[48:58]);name=header[:16].decode().strip().rstrip('/')
                members[name]=first[offset+60:offset+60+size];offset+=60+size+size%2
            with tarfile.open(fileobj=io.BytesIO(members['control.tar.gz']),mode='r:gz') as control:
                for hook in ['postinst','postrm']:
                    self.assertEqual(control.getmember('./'+hook).mode,0o755)
                    self.assertTrue(control.extractfile('./'+hook).read().startswith(b'#!/bin/sh\n'))
                self.assertIn(b'Depends: fixture-runtime',control.extractfile('./control').read())
                self.assertIn(b'/etc/kudzu-bs-analytics.conf',control.extractfile('./conffiles').read())
            with tarfile.open(fileobj=io.BytesIO(members['data.tar.gz']),mode='r:gz') as files:
                self.assertEqual(files.getmember('./etc/init.d/kudzu-bs-analytics').mode,0o755)
                self.assertEqual(files.getmember('./etc/kudzu-bs-analytics.conf').mode,0o600)
                self.assertEqual(files.getmember('./usr/bin/basicstation-analytics').mode,0o755)

    def test_host_binary_and_missing_dependencies_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);binary=root/'host';binary.write_bytes(b'not an ARM binary')
            with self.assertRaises(ValueError):packager.build(binary,root,'0.1.0','runtime',root/'out.ipk')
            with self.assertRaises(ValueError):packager.build(binary,root,'0.1.0','',root/'out.ipk')

if __name__=='__main__':unittest.main()
