# New apartment Jeedom

Use a fresh Jeedom instance with its own empty web and MariaDB volumes. Complete the normal Jeedom initialization and install **Ajax Systems** and **MQTT Manager** through the usual Jeedom interface. The local VM download currently contains only directories; it is not an installation or database backup. Do not use it to seed the apartment instance.

The public [Ajax Systems source](https://github.com/jeedom/plugin-ajaxSystem) and [MQTT Manager source](https://github.com/jeedom/plugin-mqtt2) help with compatibility review. This package remains an overlay for an already installed official Ajax Systems plugin. It does not contain the complete plugin or grant access to Market/cloud services. Preserve the original license and patch notices; see [licensing](LICENSING.md).

## Apply the reviewed telemetry patch

Do this before connecting Ajax Systems to an Ajax account. If it was already connected, disable the plugin in Jeedom and make a normal Jeedom backup first. Do not restore the house's database, `common.config.php`, tokens, API keys, cache, scenarios or equipment into the apartment.

The archive is generated locally with the existing verified packager:

```powershell
python scripts/package-jeedom.py --version 2.1.0 --timestamp 1791453600 --output tmp/apartment-jeedom-patch
```

Copy the resulting archive to a private staging directory and extract its whole `Jeedom/` folder. Preserve LF line endings. Once the container has initialized, expose that folder through a separate read-only bind mount; for example add the following to the `jeedom-appt` service, substituting the actual host staging directory:

```yaml
volumes:
  - /path/to/private/staging/Jeedom:/opt/ajaxbridge-jeedom:ro
```

Keep the normal Jeedom HTML and database mounts too. Do not mount the patch over `/var/www/html` or over the plugin directory. The staging mount contains code/documentation only; it needs no house configuration or credentials. The container must be recreated to add a mount if it is already running.

Run the read-only check inside the **new apartment container**:

```sh
docker exec jeedom-appt bash /opt/ajaxbridge-jeedom/tools/install-patch.sh \
  --check /opt/ajaxbridge-jeedom /var/www/html/plugins/ajaxSystem
```

The helper verifies bundle hashes, checks the installed plugin against recorded compatible files, lints patched PHP, and exercises the patched parser with synthetic fixtures in a private temporary directory. It does not load the real Jeedom core, database, Ajax credentials or services. If it reports `REVIEW`, stop and compare the identified files; a newer official plugin may require a reviewed merge.

Disable Ajax Systems in Jeedom before applying:

```sh
docker exec --user root jeedom-appt bash /opt/ajaxbridge-jeedom/tools/install-patch.sh \
  --apply /opt/ajaxbridge-jeedom /var/www/html/plugins/ajaxSystem \
  /var/www/html/backup/ajaxbridge-patches
```

The helper creates a private, complete original-plugin archive before replacing only the listed overlay files. Save the printed backup outside the container with `docker cp`. That archive restores plugin files; a normal Jeedom backup is still needed to roll back later database/command synchronization. The helper does not enable the plugin, synchronize, refresh, execute commands or restart services; disabling it beforehand is your responsibility. If PHP OPcache retains old code, restart the apartment container yourself after configuration.

For file rollback, keep the plugin disabled, move the patched `plugins/ajaxSystem` directory to a separate private recovery directory, recreate `plugins/ajaxSystem`, and restore the exact original-plugin archive there. Retain both copies until verified. Simply extracting the archive over the patched folder leaves newly added template files behind.

## Keep Ajax access apartment-only

Create a dedicated **ordinary Ajax user**, invited only to the apartment hub/space. Before logging in from Jeedom, verify this user cannot see the house. The plugin's synchronization imports every Ajax hub visible to its user; using the house's user can import house equipment into the apartment even with separate MQTT prefixes. The official plugin uses Jeedom cloud requests and requires an externally reachable HTTPS callback URL with a valid certificate for real-time updates. Configure a separate apartment callback URL, then log in with the dedicated Ajax user. [Official Ajax Systems guidance](https://raw.githubusercontent.com/jeedom/plugin-ajaxSystem/master/docs/fr_FR/index.md).

In MQTT Manager use **remote broker** mode, root topic `jeedom_appt`, and the apartment's broker credentials. Keep linked-Jeedom topics empty and global Home Assistant autodiscovery disabled. The broker account should deliver only the required apartment topics. Publish equipment discovery after the dedicated Ajax user's synchronization shows only apartment equipment. Ensure the bridge input/discovery/set topics use the exact same MQTT Manager root. Raw command IDs can overlap with the house. [Official MQTT Manager guidance](https://raw.githubusercontent.com/jeedom/plugin-mqtt2/master/docs/fr_FR/index.md).

## Hub 2 Jeweller direct SIA

The apartment bridge expects SIA object/account **`A4428`**, host TCP port **`9013`**, and a **60-second monitoring-station ping**. These are this deployment's values, not Ajax protocol defaults. Its AES setting must match the hub; use the apartment's own AES-128 key, never the house's key.

In the current Ajax app, select the apartment space, open its settings, then **Security companies → Monitoring station**. An administrator or a PRO with settings rights must configure it. Select **SIA DC-09 (SIA-DCS)**. Set the primary receiver IP to the reachable address of this deployment and its port to `9013`; object number to `A4428`; monitoring-station ping interval to **1 minute**. Configure restore reporting so restored device states are sent to the receiver. Direct SIA events bypass Ajax Cloud and do not include all the richer Jeedom metrics. [Official Ajax SIA setup](https://support.ajax.systems/en/how-to-use-sia-for-cms-connection/).

If using AES encryption, enable it in the hub and enter the exact same **32 hexadecimal characters / 16-byte AES-128 key** configured as `AJAXBRIDGE_ENCRYPTION_KEY` for the apartment bridge. Enter the key privately; it must never appear in screenshots, commands, logs or this guide. Disable **Connect on demand** to keep a permanent monitoring connection, as described by Ajax. AjaxBridge supports multiple frames on the same TCP connection and resets its read deadline before each frame; the 60-second ping refreshes activity within the configured timeout.

`192.168.100.100:9013` is appropriate only when the apartment hub can route to the NAS's private network. If the apartment is on another network, first configure a router-level site-to-site VPN or a suitable reachable receiver address and TCP forwarding to NAS port `9013`. A VPN running only on a laptop does not route the Ajax hub. Cellular backup cannot normally reach a private NAS address through the apartment's Ethernet VPN, so choose channel-specific reachable receivers where needed. Preserve any existing professional monitoring configuration and confirm the intended channel routing before changing it. [Ajax space/channel settings](https://support.ajax.systems/en/how-to-configure-a-space/).

After applying the settings, verify the apartment bridge sees account `A4428` and fresh ping timestamps, the new `/admin` lists apartment equipment only, and HA/card/Prometheus data use the apartment source. Do not trigger a real security alarm as an installation test; a periodic ping and normal device updates can verify reception first.
