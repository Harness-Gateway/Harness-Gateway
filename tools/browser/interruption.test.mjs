import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { access, mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import test from 'node:test';

const directory = fileURLToPath(new URL('.', import.meta.url));
const exists = path => access(path).then(() => true, () => false);
async function running(pid) {
  try { return (await readFile(`/proc/${pid}/stat`, 'utf8')).split(' ')[2] !== 'Z'; } catch (error) {
    if (error.code === 'ENOENT') return false;
    throw error;
  }
}
async function until(check, message, timeout = 10000) {
  const deadline = Date.now() + timeout;
  while (!(await check())) {
    if (Date.now() > deadline) throw new Error(`Timed out: ${message}`);
    await delay(25);
  }
}

test('targeted browser-runner signals retire owned processes and save diagnostics', { skip: process.platform !== 'linux', timeout: 180000 }, async t => {
  for (const [mode, signal] of [['launch', 'SIGTERM'], ['close-failure', 'SIGTERM'], ['codex', 'SIGTERM'], ['copilot', 'SIGINT']]) {
    await t.test(`${mode}/${signal}`, async t => {
      const work = await mkdtemp(join(tmpdir(), 'gateway-browser-interruption-'));
      const ready = join(work, 'ready.json');
      const output = join(work, 'artifacts');
      const child = spawn(process.execPath, ['--loader', join(directory, 'testdata/pause-loader.mjs'), join(directory, 'acceptance.mjs')], {
        env: { ...process.env, HARNESS_GATEWAY_BROWSER_TEST_MODE: mode, HARNESS_GATEWAY_BROWSER_TEST_READY: ready, HARNESS_GATEWAY_BROWSER_OUTPUT_DIR: output },
      });
      let log = '', info, result;
      child.stdout.on('data', data => { log += data; });
      child.stderr.on('data', data => { log += data; });
      const done = new Promise((resolve, reject) => {
        child.once('error', reject);
        child.once('close', (code, signal) => { result = { code, signal }; resolve(); });
      });
      done.catch(() => {});
      t.after(async () => {
        if (!result) child.kill('SIGKILL');
        await done;
        // Emergency fixture cleanup if the regression fails. Do not touch the
        // user's service or any process outside this disposable runner.
        if (info && await running(info.gatewayPID)) process.kill(info.gatewayPID, 'SIGINT');
        if (info) {
          for (const pid of info.pids) if (await running(pid)) process.kill(Number(pid), 'SIGKILL');
          await rm(info.runtime, { recursive: true, force: true });
        }
        await rm(work, { recursive: true, force: true });
      });
      await until(async () => {
        if (result) throw new Error(`runner exited before fixture readiness: ${JSON.stringify(result)}\n${log}`);
        if (!(await exists(ready))) return false;
        info = JSON.parse(await readFile(ready, 'utf8'));
        return true;
      }, 'actual gateway/stream readiness', 60000);
      assert(await running(info.gatewayPID));
      for (const pid of info.pids) assert(await running(pid), 'stream fixture must be active before interruption');
      child.kill(signal);
      if (mode === 'launch') { await delay(100); child.kill(signal); } // idempotent signal handling
      await until(() => result, 'bounded runner shutdown', 18000);
      await done;
      assert.deepEqual(result, { code: signal === 'SIGINT' ? 130 : 143, signal: null }, log);
      const report = JSON.parse(await readFile(join(output, 'acceptance.json'), 'utf8'));
      assert.equal(report.passed, false);
      assert.equal(report.interrupted, signal);
      assert.equal(report.gatewayPID, info.gatewayPID);
      assert.equal(await exists(info.runtime), false);
      assert.equal(await running(info.gatewayPID), false, 'gateway survived runner interruption');
      for (const pid of info.pids) assert.equal(await running(pid), false, 'active CLI survived runner interruption');
      assert((await readFile(join(output, 'gateway.log'), 'utf8')).includes('Harness-Gateway listening'));
      if (mode === 'close-failure') assert(report.cleanupFailures.some(text => text.includes('injected browser close failure')));
      if (mode === 'launch') assert(report.cleanupFailures.some(text => text.includes('browser launch settlement')));
      if (mode === 'codex' || mode === 'copilot') assert.equal(report.cleanupFailures, undefined, log);
    });
  }
});
