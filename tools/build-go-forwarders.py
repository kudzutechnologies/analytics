#!/usr/bin/env python3
"""Build portable Go forwarders with bounds checks and deterministic archives."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile

ROOT=Path(__file__).resolve().parent.parent
TARGETS={'amd64':{'GOARCH':'amd64'},'rpi':{'GOARCH':'arm','GOARM':'5'},'arm6':{'GOARCH':'arm','GOARM':'6'},'arm7':{'GOARCH':'arm','GOARM':'7'},'mipsle-24kec-v5':{'GOARCH':'mipsle','GOMIPS':'softfloat'},'mips-24kec-v5':{'GOARCH':'mips','GOMIPS':'softfloat'}}

def tarball(path,entries):
    with path.open('wb') as raw:
        with gzip.GzipFile(fileobj=raw,mode='wb',filename='',mtime=0) as compressed:
            with tarfile.open(fileobj=compressed,mode='w') as tar:
                for name,source,mode in entries:
                    info=tarfile.TarInfo(name);info.size=source.stat().st_size;info.mode=mode;info.mtime=0;info.uid=info.gid=0;info.uname=info.gname='root'
                    with source.open('rb') as content:tar.addfile(info,content)

p=argparse.ArgumentParser(description=__doc__);p.add_argument('--component',choices=['bs','udp'],required=True);p.add_argument('--target',choices=['all',*TARGETS],default='all');p.add_argument('--version',default='dev');p.add_argument('--output-dir',type=Path,required=True);p.add_argument('--bundle',action='store_true');a=p.parse_args()
if any(c.isspace() for c in a.version) or not a.version:p.error('version must be one non-empty token')
component='forwarder-bs' if a.component=='bs' else 'forwarder';name='kudzu-'+component
out=a.output_dir.resolve();out.mkdir(parents=True,exist_ok=True)
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
dirty=bool(subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=ROOT,text=True).strip())
artifacts=[];binaries=[]
for target in (TARGETS if a.target=='all' else [a.target]):
    binary=out/(name+'-'+target);env=dict(os.environ);env.update({'CGO_ENABLED':'0','GOOS':'linux','GOARM':'','GOMIPS':'',**TARGETS[target]})
    subprocess.run(['go','build','-trimpath','-ldflags','-s -w -X main.MajorVersion='+a.version,'-o',str(binary),'./'+component+('/cli' if a.component=='bs' else '')],cwd=ROOT,env=env,check=True)
    entries=[(binary.name,binary,0o755),('README.md',ROOT/component/('cli/README.md' if a.component=='bs' else 'README.md'),0o644),('LICENSE',ROOT/'LICENSE',0o644)]
    archive=out/(binary.name+'.tgz');tarball(archive,entries);binaries.append((binary.name,binary,0o755))
    for file in [binary,archive]:artifacts.append({'name':file.name,'target':target,'bytes':file.stat().st_size,'sha256':hashlib.sha256(file.read_bytes()).hexdigest()})
if a.bundle:tarball(out/(name+'-all.tar.gz'),binaries)
manifest={'version':a.version,'source_commit':revision,'source_dirty':dirty,'go':subprocess.check_output(['go','version'],text=True).strip(),'CGO_ENABLED':0,'artifacts':artifacts}
(out/(name+'-manifest'+('' if a.target=='all' else '-'+a.target)+'.json')).write_text(json.dumps(manifest,indent=2)+'\n')
