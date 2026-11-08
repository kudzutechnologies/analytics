#!/usr/bin/env python3
"""Build a deterministic Kerlink IPK from an explicitly supplied ARM binary."""
import argparse
import gzip
import io
from pathlib import Path
import re
import struct
import tarfile

def archive(entries):
    out=io.BytesIO()
    with gzip.GzipFile(fileobj=out,mode='wb',filename='',mtime=0) as compressed:
        with tarfile.open(fileobj=compressed,mode='w',format=tarfile.GNU_FORMAT) as tar:
            for name,data,mode in sorted(entries):
                info=tarfile.TarInfo('./'+name);info.size=len(data);info.mode=mode;info.mtime=0;info.uid=info.gid=0;info.uname=info.gname='root'
                tar.addfile(info,io.BytesIO(data))
    return out.getvalue()

def build(binary,assets,version,depends,output):
    if not re.fullmatch(r'[0-9][0-9A-Za-z.+~:-]*',version):raise ValueError('version must be an explicit package version')
    if not depends.strip() or '\n' in depends:raise ValueError('supply target SDK runtime package dependencies')
    data=binary.read_bytes()
    if len(data)<52 or data[:6]!=b'\x7fELF\x01\x01' or struct.unpack_from('<H',data,18)[0]!=40:raise ValueError('Kerlink requires a target-built little-endian ARM32 ELF binary')
    entries=[];conffiles=[]
    for path in sorted((assets/'etc').rglob('*')):
        if path.is_symlink():raise ValueError('package assets must not contain symlinks')
        if not path.is_file():continue
        name=path.relative_to(assets).as_posix();mode=0o755 if '/init.d/' in name else 0o600 if name.endswith('.conf') or '/default/' in name else 0o644
        entries.append((name,path.read_bytes(),mode))
        if '/init.d/' not in name:conffiles.append('/'+name)
    entries.append(('usr/bin/basicstation-analytics',data,0o755))
    control=f'Package: kudzu-bs-analytics\nVersion: {version}\nArchitecture: klkgw\nMaintainer: Kudzu Technologies <info@kudzu.gr>\nDepends: {depends}\nSection: analytics\nPriority: optional\nDescription: Bounded Basic Station analytics consumer\n'
    postinst=b'''#!/bin/sh
set -eu
root=${IPKG_INSTROOT:-}
mkdir -p "$root/etc/rcU.d" "$root/etc/rcK.d"
ln -sf ../init.d/kudzu-bs-analytics "$root/etc/rcU.d/S53kudzu-bs-analytics"
ln -sf ../init.d/kudzu-bs-analytics "$root/etc/rcK.d/K53kudzu-bs-analytics"
'''
    postrm=b'''#!/bin/sh
set -eu
root=${IPKG_INSTROOT:-}
rm -f "$root/etc/rcU.d/S53kudzu-bs-analytics" "$root/etc/rcK.d/K53kudzu-bs-analytics"
'''
    members=[('debian-binary',b'2.0\n'),('control.tar.gz',archive([('control',control.encode(),0o644),('conffiles',('\n'.join(conffiles)+'\n').encode(),0o644),('postinst',postinst,0o755),('postrm',postrm,0o755)])),('data.tar.gz',archive(entries))]
    output.parent.mkdir(parents=True,exist_ok=True)
    with output.open('wb') as dest:
        dest.write(b'!<arch>\n')
        for name,body in members:
            header=f'{name+"/":<16}{0:<12}{0:<6}{0:<6}{"100644":<8}{len(body):<10}`\n'.encode('ascii')
            assert len(header)==60
            dest.write(header);dest.write(body)
            if len(body)%2:dest.write(b'\n')

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--binary',type=Path,required=True);p.add_argument('--assets',type=Path,required=True);p.add_argument('--version',required=True);p.add_argument('--depends',required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
    try:build(a.binary,a.assets,a.version,a.depends,a.output)
    except (ValueError,OSError) as e:p.exit(2,f'IPK build blocked: {e}\n')
