import { writeFileSync } from 'node:fs';
export const chromium = { launch: async () => {
 writeFileSync(process.env.EGM_R3_BROWSER_READY, JSON.stringify({node_pid:process.pid}));
 await new Promise(() => setInterval(() => {},1000));
}};
