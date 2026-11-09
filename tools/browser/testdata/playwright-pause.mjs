// Only the interruption test loads this module. Gateway/CLI processes and HTTP
// traffic remain real; browser UI work pauses at a deterministic resource boundary.
import childProcess from 'node:child_process';
import { syncBuiltinESMExports } from 'node:module';
import { readdir, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const spawn = childProcess.spawn;
let gateway, base, runtime;
childProcess.spawn = (...args) => {
  const child = spawn(...args);
  if (args[0].endsWith('/gateway')) {
    gateway = child;
    runtime = dirname(args[0]);
    let log = '';
    child.stderr.on('data', data => {
      log += data;
      base = log.match(/url=(http:\/\/127\.0\.0\.1:\d+)/)?.[1];
    });
  }
  return child;
};
syncBuiltinESMExports();

const pause = () => new Promise(() => setInterval(() => {}, 1000));
export const chromium = { launch: async () => {
  if (!gateway || !base) throw new Error('test reached browser launch without actual gateway readiness');
  const mode = process.env.HARNESS_GATEWAY_BROWSER_TEST_MODE;
  let reader;
  if (mode === 'codex' || mode === 'copilot') {
    const response = await fetch(`${base}/v1/chat/completions`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: mode, messages: [{ role: 'system', content: 'Be brief' }, { role: 'user', content: 'hold' }], stream: true }),
    });
    if (response.status !== 200) throw new Error(`stream failed: ${response.status}`);
    reader = response.body.getReader();
    let text = '';
    while (!text.includes('ready')) {
      const chunk = await reader.read();
      if (chunk.done) throw new Error('stream ended before hold marker');
      text += new TextDecoder().decode(chunk.value);
    }
  }
  let pids = [];
  for (let attempt = 0; attempt < 100; attempt++) {
    pids = await readdir(`${runtime}/pids`);
    if (!reader || pids.length) break;
    await delay(10);
  }
  if (reader && !pids.length) throw new Error('active stream had no recorded CLI PID');
  const ready = process.env.HARNESS_GATEWAY_BROWSER_TEST_READY;
  await writeFile(`${ready}.tmp`, JSON.stringify({ gatewayPID: gateway.pid, runtime, pids }));
  await rename(`${ready}.tmp`, ready);
  if (mode === 'launch') return pause();
  return {
    version: () => 'interruption-test-pause',
    newContext: pause,
    close: async () => {
      if (reader) await reader.cancel().catch(() => {});
      if (mode === 'close-failure') throw new Error('injected browser close failure');
    },
  };
} };
