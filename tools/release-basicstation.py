#!/usr/bin/env python3
"""Release only with an exact tag, pinned SDKs/producer and hardware evidence."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT=Path(__file__).resolve().parent.parent
TARGETS={'corecell':'arm-linux-gnueabihf','rpi':'arm-linux-gnueabihf','kerlink':'arm-klk-linux-gnueabi'}

def git(*args):return subprocess.check_output(['git',*args],cwd=ROOT,text=True,stderr=subprocess.DEVNULL).strip()
def inputs():
    required=['BASICSTATION_SDK_MANIFEST','BASICSTATION_PRODUCER_HEADER','BASICSTATION_PRODUCER_REF','BASICSTATION_HARDWARE_EVIDENCE']
    missing=[key for key in required if not os.environ.get(key)]
    if missing:raise ValueError('missing release prerequisites: '+', '.join(missing))
    sdk=json.loads(Path(os.environ[required[0]]).read_text())
    producer=Path(os.environ[required[1]]).resolve();ref=os.environ[required[2]]
    if not re.fullmatch(r'[0-9a-fA-F]{40}',ref) or not producer.is_file():raise ValueError('supply the pinned producer header and full commit SHA')
    evidence=json.loads(Path(os.environ[required[3]]).read_text())
    for name,prefix in TARGETS.items():
        entry=sdk.get(name,{})
        for key in ['toolchain_root','sysroot','host_protoc','host_grpc_plugin','protobuf_version','grpc_version','runtime_dependencies']:
            if not entry.get(key) or 'REPLACE' in str(entry[key]):raise ValueError(f'{name}: missing {key}')
        for key in ['toolchain_root','sysroot','host_protoc','host_grpc_plugin']:
            if not Path(entry[key]).exists():raise ValueError(f'{name}: unavailable {key}')
        if not (Path(entry['toolchain_root'])/'bin'/f'{prefix}-g++').is_file():raise ValueError(f'{name}: target compiler unavailable')
        proof=evidence.get(name,{})
        if proof.get('producer_ref')!=ref or not all(proof.get('checks',{}).get(check) is True for check in ['install','start','stop','upgrade','radio','minimum-memory','outage']):raise ValueError(f'{name}: incomplete hardware test evidence')
        if proof.get('passed') is not True or not re.fullmatch(r'[0-9a-f]{64}',proof.get('binary_sha256','')):raise ValueError(f'{name}: require install/start/stop/upgrade/radio smoke evidence and exact binary SHA256')
    return sdk,producer,ref,evidence

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--check',action='store_true');parser.add_argument('--check-inputs',action='store_true');args=parser.parse_args()
    try:
        if args.check_inputs:
            inputs();print('Embedded release inputs available');return 0
        try:tag=git('describe','--exact-match','--tags','HEAD')
        except subprocess.CalledProcessError:raise ValueError('release requires HEAD on an exact version tag')
        if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?',tag):raise ValueError('release tag must be a semantic version prefixed with v')
        if git('status','--porcelain'):raise ValueError('release requires a clean checkout')
        sdk,producer,ref,evidence=inputs()
        if args.check:print('Release prerequisites available; target compilation/ABI checks still required');return 0
        version=tag[1:];output=ROOT/'releases'/tag;output.mkdir(parents=True,exist_ok=True)
        records=[]
        for name,prefix in TARGETS.items():
            entry=sdk[name];build=ROOT/'basicstation-analytics'/('build-release-'+name)
            abi=[sys.executable,str(ROOT/'tools/check-basicstation-abi.py'),'--producer-header',str(producer),'--producer-ref',ref,'--cxx',str(Path(entry['toolchain_root'])/'bin'/f'{prefix}-g++'),'--sysroot',entry['sysroot']]
            for directory in entry.get('producer_include_dirs',[]):abi+=['--include-dir',directory]
            subprocess.run(abi,cwd=ROOT,check=True)
            subprocess.run(['cmake','-S',str(ROOT/'basicstation-analytics'),'-B',str(build),'-DCMAKE_BUILD_TYPE=MinSizeRel',f'-DCMAKE_TOOLCHAIN_FILE={ROOT}/basicstation-analytics/cmake/toolchain-{name}.cmake',f'-DTOOLCHAIN_ROOT={entry["toolchain_root"]}',f'-DCMAKE_SYSROOT={entry["sysroot"]}',f'-DCLIENT_CC_HOST_PROTOC={entry["host_protoc"]}',f'-DCLIENT_CC_HOST_GRPC_PLUGIN={entry["host_grpc_plugin"]}',f'-DBS_ANALYTICS_VERSION={version}'],check=True)
            subprocess.run(['cmake','--build',str(build),'--target','basicstation-analytics'],check=True)
            binary=build/'basicstation-analytics'
            subprocess.run([str(Path(entry['toolchain_root'])/'bin'/f'{prefix}-strip'),str(binary)],check=True)
            dependency_versions=json.loads((build/'client-cc/dependency-versions.json').read_text())
            if dependency_versions['protobuf']!=entry['protobuf_version'] or dependency_versions['grpc']!=entry['grpc_version']:raise ValueError(f'{name}: dependency versions differ from SDK manifest')
            digest=hashlib.sha256(binary.read_bytes()).hexdigest()
            if digest!=evidence[name]['binary_sha256']:raise ValueError(f'{name}: compiled binary differs from hardware-tested candidate')
            destination=output/('basicstation-analytics-'+name);shutil.copy2(binary,destination)
            records.append({'target':name,'binary':destination.name,'sha256':digest,'bytes':destination.stat().st_size,'sdk':entry})
            if name=='kerlink':subprocess.run([sys.executable,str(ROOT/'tools/build_ipk.py'),'--binary',str(binary),'--assets',str(ROOT/'basicstation-analytics/packaging/kerlink'),'--version',version,'--depends',entry['runtime_dependencies'],'--output',str(output/f'kudzu-bs-analytics_{version}_klkgw.ipk')],check=True)
        for component in ['udp','bs']:subprocess.run([sys.executable,str(ROOT/'tools/build-go-forwarders.py'),'--component',component,'--version',version,'--output-dir',str(output)],cwd=ROOT,check=True)
        # Preserve existing UDP IPK/removal-package generation. Its tooling must
        # be installed separately; failures are fatal rather than omitted.
        subprocess.run(['make','-C',str(ROOT/'forwarder/pkg/kerlink'),'all',f'PKG_VERSION={version}',f'BIN_DIR={output}'],check=True)
        for name in [f'kudzu-forwarder_{version}_klkgw.ipk','kudzu-forwarder-remove.ipk']:
            shutil.copy2(ROOT/'forwarder/pkg/kerlink'/name,output/name)
        manifest={'tag':tag,'source_commit':git('rev-parse','HEAD'),'producer_ref':ref,'cpp':records}
        (output/'release-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
        (output/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in sorted(output.iterdir()) if p.is_file() and p.name!='SHA256SUMS'))
        if git('status','--porcelain'):raise ValueError('release build modified repository files')
        print(f'Release artifacts: {output}')
        return 0
    except (ValueError,OSError,subprocess.CalledProcessError) as exc:
        print('Release blocked: '+str(exc),file=sys.stderr);return 2
if __name__=='__main__':raise SystemExit(main())
