import fs from 'node:fs';

const mode = process.argv[2];
if (mode === 'configure') {
  fs.mkdirSync('/var/lib/jeedom-external/probe-private', { recursive: true, mode: 0o700 });
  fs.writeFileSync('/var/lib/jeedom-external/probe-private/secret', 'synthetic fixture only');
} else if (mode === 'service') {
  if (process.argv[4] === 'start') {
    fs.writeFileSync('/tmp/probe-' + process.argv[3] + '.json', JSON.stringify({ umask: process.umask().toString(8) }));
  }
} else if (mode === 'cron') {
  fs.mkdirSync('/tmp/jeedom/mqtt2', { recursive: true, mode: 0o774 });
  fs.chownSync('/tmp/jeedom/mqtt2', 33, 33);
  // Same Node API/default-mode contract as MQTT Manager's Jeedom.write_pid.
  fs.writeFileSync('/tmp/jeedom/mqtt2/deamon.pid', String(process.pid));
  fs.writeFileSync('/tmp/probe-cron.json', JSON.stringify({ umask: process.umask().toString(8) }));
  setInterval(() => {}, 1000);
} else if (mode === 'inspect') {
  const stat = file => {
    const result = fs.statSync(file);
    return { mode: (result.mode & 0o777).toString(8), uid: result.uid, gid: result.gid };
  };
  console.log(JSON.stringify({
    pid: stat('/tmp/jeedom/mqtt2/deamon.pid'),
    private_dir: stat('/var/lib/jeedom-external/probe-private'),
    secret: stat('/var/lib/jeedom-external/probe-private/secret'),
    services: Object.fromEntries(['atd', 'apache2', 'cron'].map(name => [
      name, JSON.parse(fs.readFileSync('/tmp/probe-' + name + '.json', 'utf8')),
    ])),
  }));
}
