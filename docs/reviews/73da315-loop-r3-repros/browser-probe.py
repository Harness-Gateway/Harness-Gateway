import os,subprocess,time,json,signal,urllib.request
from pathlib import Path
root=Path.cwd();p=root/'.egm-r3-probes';ready=p/'browser-ready.json';art=p/'browser-artifacts';log=p/'browser-probe.log'
env=os.environ.copy();env.update(TMPDIR=str(p/'tmp'),GOTMPDIR=str(p/'tmp'),GOCACHE=str(p/'go-cache'),EGM_R3_BROWSER_READY=str(ready),HARNESS_GATEWAY_BROWSER_OUTPUT_DIR=str(art))
gateway_pid=None;runner=None
try:
 with log.open('w') as out:
  runner=subprocess.Popen(['node','--loader',str(p/'loader.mjs'),str(root/'tools/browser/acceptance.mjs')],env=env,stdout=out,stderr=out,start_new_session=True)
  deadline=time.monotonic()+120
  while not ready.exists():
   if runner.poll() is not None: raise RuntimeError('runner exited before controlled pause: '+log.read_text())
   if time.monotonic()>deadline:raise RuntimeError('readiness timeout')
   time.sleep(.05)
  children=Path(f'/proc/{runner.pid}/task/{runner.pid}/children').read_text().split()
  for child in children:
   args=Path(f'/proc/{child}/cmdline').read_bytes().split(b'\0')
   if args and args[0].endswith(b'/gateway'):gateway_pid=int(child);runtime=Path(os.fsdecode(args[0])).parent
  if not gateway_pid:raise RuntimeError('gateway child missing')
  os.kill(runner.pid,signal.SIGTERM);rc=runner.wait(timeout=10);time.sleep(.3)
  running=Path(f'/proc/{gateway_pid}/stat').read_text().split()[2]!='Z'
  receipt={'node_exit':rc,'node_pid':runner.pid,'gateway_pid':gateway_pid,'gateway_still_running':running,'runtime_directory_retained':runtime.exists(),'acceptance_receipt_written':(art/'acceptance.json').exists(),'pause':'Playwright launch stub after actual gateway readiness; no browser launched','source':'unaltered tools/browser/acceptance.mjs with dependency loader hook'}
  print(json.dumps(receipt,indent=2));(p/'browser-proof.json').write_text(json.dumps(receipt,indent=2)+'\n')
  assert rc==-signal.SIGTERM and running and runtime.exists() and not (art/'acceptance.json').exists()
finally:
 if gateway_pid:
  try:os.kill(gateway_pid,signal.SIGINT)
  except ProcessLookupError:pass
  time.sleep(.3)
 if runner:
  try:os.killpg(runner.pid,signal.SIGKILL)
  except ProcessLookupError:pass
