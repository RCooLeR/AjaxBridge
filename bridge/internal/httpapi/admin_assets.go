package httpapi

const adminHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AjaxBridge Admin</title>
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css" integrity="sha384-sRIl4kxILFvY47J16cr9ZwB07vP4J8+LH7qKQnuqkuIAvNWLzeN8tE5YBujZqJLB" crossorigin="anonymous" rel="stylesheet">
  <style>
    body { background: #f6f7f9; }
    .brand-logo { width: 44px; height: 44px; object-fit: contain; }
    .table input, .table select, .table textarea { min-width: 120px; }
    .table textarea { min-height: 38px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .status-dot { width: .65rem; height: .65rem; border-radius: 999px; display: inline-block; }
    .status-on { background: #198754; }
    .status-off { background: #adb5bd; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
    .small-table td, .small-table th { font-size: .875rem; }
    .matching-summary .badge { font-size: .8rem; font-weight: 500; }
    .jeedom-command-list { min-width: 760px; }
    .jeedom-command-row {
      display: grid;
      grid-template-columns: minmax(4.5rem, .55fr) minmax(8rem, 1fr) minmax(9rem, 1.1fr) minmax(7rem, .8fr) minmax(12rem, 1.5fr);
      gap: .5rem;
      align-items: start;
      padding: .4rem .25rem;
      border-bottom: 1px solid var(--bs-border-color);
    }
    .jeedom-command-row:last-child { border-bottom: 0; }
    .jeedom-command-head { color: var(--bs-secondary-color); font-size: .72rem; font-weight: 600; text-transform: uppercase; }
    .jeedom-command-cell { min-width: 0; overflow-wrap: anywhere; }
    .jeedom-command-cell small { display: block; color: var(--bs-secondary-color); }
    .command-badges { display: flex; flex-wrap: wrap; gap: .2rem; margin-top: .2rem; }
    .command-badges .badge { font-size: .65rem; font-weight: 500; }
  </style>
</head>
<body>
  <nav class="navbar navbar-expand-lg bg-white border-bottom sticky-top">
    <div class="container-fluid">
      <a class="navbar-brand d-flex align-items-center gap-2" href="/admin">
        <img src="/admin/logo.png" class="brand-logo" alt="AjaxBridge">
        <span>AjaxBridge Admin</span>
      </a>
      <div class="d-flex gap-2">
        <button class="btn btn-outline-secondary btn-sm" onclick="loadAll()">Refresh</button>
      </div>
    </div>
  </nav>

  <main class="container-fluid py-3">
    <div id="alertBox"></div>
    <ul class="nav nav-tabs" role="tablist">
      <li class="nav-item"><button class="nav-link active" data-bs-toggle="tab" data-bs-target="#devicesTab" type="button">Devices</button></li>
      <li class="nav-item"><button class="nav-link" data-bs-toggle="tab" data-bs-target="#matchingTab" type="button">SIA / Jeedom Matching</button></li>
      <li class="nav-item"><button class="nav-link" data-bs-toggle="tab" data-bs-target="#notificationsTab" type="button">Notifications</button></li>
      <li class="nav-item"><button class="nav-link" data-bs-toggle="tab" data-bs-target="#statusTab" type="button">Status</button></li>
    </ul>

    <div class="tab-content bg-white border border-top-0 p-3">
      <section class="tab-pane fade show active" id="devicesTab">
        <div class="d-flex justify-content-between align-items-center mb-3">
          <div>
            <h1 class="h5 mb-1">Device Catalog</h1>
            <div class="text-body-secondary small">Edit names, rooms, kinds, SIA zone metadata, and expected event signals.</div>
          </div>
          <div class="d-flex gap-2">
            <button class="btn btn-outline-primary btn-sm" onclick="addDeviceRow()">Add device</button>
            <button class="btn btn-primary btn-sm" onclick="saveDevices()">Save catalog</button>
          </div>
        </div>
        <div class="table-responsive">
          <table class="table table-sm table-hover align-middle" id="devicesTable">
            <thead>
              <tr>
                <th>Account</th><th>Zone</th><th>Device</th><th>Name</th><th>Room</th><th>Kind</th><th>Events</th><th>Jeedom names</th><th>Jeedom command IDs</th><th></th>
              </tr>
            </thead>
            <tbody></tbody>
          </table>
        </div>
      </section>

      <section class="tab-pane fade" id="matchingTab">
        <div id="matchingSummary" class="matching-summary d-flex flex-wrap gap-2 mb-3" aria-live="polite"></div>
        <div class="row g-3">
          <div class="col-12">
            <h2 class="h5">SIA Devices</h2>
            <div class="table-responsive">
              <table class="table table-sm small-table" id="siaTable">
                <thead><tr><th>SIA key</th><th>Name</th><th>Room</th><th>Kind</th><th>Jeedom command IDs</th></tr></thead>
                <tbody></tbody>
              </table>
            </div>
          </div>
          <div class="col-12">
            <h2 class="h5">Jeedom Devices</h2>
            <div class="table-responsive">
              <table class="table table-sm small-table" id="jeedomTable">
                <thead><tr><th>Slug</th><th>Name</th><th>Type</th><th>Linked</th><th>Commands</th></tr></thead>
                <tbody></tbody>
              </table>
            </div>
          </div>
        </div>
      </section>

      <section class="tab-pane fade" id="notificationsTab">
        <div class="d-flex justify-content-between align-items-center mb-3">
          <div>
            <h2 class="h5 mb-1">Notification Rules</h2>
            <div class="text-body-secondary small">Rules run on Jeedom metrics and controls. SIA arm mode can be used as a rule filter.</div>
          </div>
          <div class="form-check form-switch">
            <input class="form-check-input" type="checkbox" id="notificationsEnabled">
            <label class="form-check-label" for="notificationsEnabled">Enabled</label>
          </div>
        </div>

        <div class="mb-4">
          <div class="d-flex justify-content-between align-items-center">
            <h3 class="h6">Channels</h3>
            <button class="btn btn-outline-primary btn-sm" onclick="addChannelRow()">Add channel</button>
          </div>
          <div class="table-responsive">
            <table class="table table-sm align-middle" id="channelsTable">
              <thead><tr><th>ID</th><th>Type</th><th>URL</th><th>Method</th><th>Headers JSON</th><th></th></tr></thead>
              <tbody></tbody>
            </table>
          </div>
        </div>

        <div class="mb-4">
          <div class="d-flex justify-content-between align-items-center">
            <h3 class="h6">Rules</h3>
            <button class="btn btn-outline-primary btn-sm" onclick="addRuleRow()">Add rule</button>
          </div>
          <div class="table-responsive">
            <table class="table table-sm align-middle" id="rulesTable">
              <thead>
                <tr>
                  <th>On</th><th>Name</th><th>Device</th><th>Metric</th><th>Condition</th><th>Value</th><th>Arm modes</th><th>Channels</th><th>Cooldown</th><th></th>
                </tr>
              </thead>
              <tbody></tbody>
            </table>
          </div>
          <div class="d-flex gap-2">
            <button class="btn btn-primary btn-sm" onclick="saveNotifications()">Save notifications</button>
            <button class="btn btn-outline-secondary btn-sm" onclick="addPresetRules()">Add common presets</button>
          </div>
        </div>

        <h3 class="h6">Recent deliveries</h3>
        <div class="table-responsive">
          <table class="table table-sm small-table" id="historyTable">
            <thead><tr><th>Time</th><th>Rule</th><th>Channel</th><th>Device</th><th>Event</th><th>Result</th><th>Message</th></tr></thead>
            <tbody></tbody>
          </table>
        </div>
      </section>

      <section class="tab-pane fade" id="statusTab">
        <div class="row g-3">
          <div class="col-12 col-lg-4">
            <h2 class="h5">Files</h2>
            <dl class="small">
              <dt>Device catalog</dt><dd class="mono" id="devicesPath"></dd>
              <dt>Notifications</dt><dd class="mono" id="notificationsPath"></dd>
            </dl>
          </div>
          <div class="col-12 col-lg-8">
            <h2 class="h5">Accounts</h2>
            <div class="table-responsive">
              <table class="table table-sm small-table" id="accountsTable">
                <thead><tr><th>Account</th><th>Online</th><th>Mode</th><th>Alarm</th><th>Tamper</th><th>Trouble</th><th>Last event</th></tr></thead>
                <tbody></tbody>
              </table>
            </div>
          </div>
        </div>
      </section>
    </div>
  </main>

  <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/js/bootstrap.bundle.min.js" integrity="sha384-FKyoEForCGlyvwx9Hj09JcYn3nv7wiPVlz7YYwJrWVcXK/BmnVDxM+D2scQbITxI" crossorigin="anonymous"></script>
  <script>
    let model = null;
    const CORE_NOTIFICATION_METRICS = [
      'temperature_c', 'humidity_percent', 'co2_ppm', 'voltage_v', 'power_w', 'current_a', 'energy_kwh',
      'state', 'status', 'issue_count', 'alarm', 'alarm_active', 'valve', 'valve_position',
      'smoke_alarm', 'critical_smoke_alarm', 'heat_alarm', 'rapid_temperature_rise_alarm',
      'carbon_monoxide_alarm', 'critical_carbon_monoxide_alarm',
      'battery_percent', 'battery_state', 'battery_low', 'online', 'external_power', 'signal_dbm', 'signal_level',
      'firmware_version', 'operating_mode', 'operating_state', 'battery_check_status', 'device_last_update'
    ];

    function esc(value) {
      return String(value ?? '').replace(/[&<>"']/g, function(ch) {
        return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[ch];
      });
    }

    function csv(values) {
      return (values || []).join(', ');
    }

    function splitCSV(value) {
      return String(value || '').split(',').map(function(v) { return v.trim(); }).filter(Boolean);
    }

    function alertMsg(type, text) {
      document.getElementById('alertBox').innerHTML = '<div class="alert alert-' + type + ' alert-dismissible fade show" role="alert">' + esc(text) + '<button type="button" class="btn-close" data-bs-dismiss="alert"></button></div>';
    }

    async function loadAll() {
      const resp = await fetch('/api/admin/bootstrap');
      if (!resp.ok) throw new Error(await resp.text());
      model = await resp.json();
      renderAll();
    }

    function renderAll() {
      renderDevices();
      renderMatching();
      renderNotifications();
      renderStatus();
    }

    function renderDevices() {
      const tbody = document.querySelector('#devicesTable tbody');
      tbody.innerHTML = '';
      (model.devices || []).forEach(function(device) {
        appendDeviceRow(device);
      });
    }

    function appendDeviceRow(device) {
      const tbody = document.querySelector('#devicesTable tbody');
      const tr = document.createElement('tr');
      tr.innerHTML =
        '<td><input class="form-control form-control-sm" data-field="account" value="' + esc(device.account) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="zone" value="' + esc(device.zone) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="device" value="' + esc(device.device) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="name" value="' + esc(device.name) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="room" value="' + esc(device.room) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="kind" value="' + esc(device.kind) + '"></td>' +
        '<td><textarea class="form-control form-control-sm" data-field="events">' + esc(csv(device.events)) + '</textarea></td>' +
        '<td><textarea class="form-control form-control-sm" data-field="jeedom_names">' + esc(csv(device.jeedom_names)) + '</textarea></td>' +
        '<td><textarea class="form-control form-control-sm" data-field="jeedom_command_ids">' + esc(csv(device.jeedom_command_ids)) + '</textarea></td>' +
        '<td><button class="btn btn-outline-danger btn-sm" onclick="this.closest(\'tr\').remove()">x</button></td>';
      tbody.appendChild(tr);
    }

    function addDeviceRow() {
      appendDeviceRow({account:'', zone:'', device:'', name:'', room:'', kind:'', events:[], jeedom_names:[], jeedom_command_ids:[]});
    }

    function collectDevices() {
      return Array.from(document.querySelectorAll('#devicesTable tbody tr')).map(function(tr) {
        const value = function(field) { return tr.querySelector('[data-field="' + field + '"]').value.trim(); };
        return {
          account: value('account'),
          zone: value('zone'),
          device: value('device'),
          name: value('name'),
          room: value('room'),
          kind: value('kind'),
          events: splitCSV(value('events')),
          jeedom_names: splitCSV(value('jeedom_names')),
          jeedom_command_ids: splitCSV(value('jeedom_command_ids'))
        };
      }).filter(function(device) { return device.account || device.zone || device.name; });
    }

    async function saveDevices() {
      const resp = await fetch('/api/admin/devices', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(collectDevices())});
      if (!resp.ok) return alertMsg('danger', await resp.text());
      const payload = await resp.json();
      model.devices = payload.devices || [];
      model.state = payload.state || model.state;
      renderAll();
      alertMsg('success', 'Device catalog saved');
    }

    function renderMatching() {
      const sia = document.querySelector('#siaTable tbody');
      sia.innerHTML = '';
      accountRows().forEach(function(account) {
        const catalog = account.catalog || {};
        const key = 'Account ' + account.account;
        const name = catalog.name || account.name || ('Ajax account ' + account.account);
        const kind = catalog.kind || 'Hub';
        const commands = csv(catalog.jeedom_command_ids || []);
        const action = catalog.exists
          ? '<button class="btn btn-outline-secondary btn-sm" data-account="' + esc(account.account) + '" onclick="focusCatalogRow(this.dataset.account, \'\')">Edit</button>'
          : '<button class="btn btn-outline-primary btn-sm" data-account="' + esc(account.account) + '" onclick="addAccountCatalogRow(this.dataset.account)">Add match row</button>';
        sia.insertAdjacentHTML('beforeend', '<tr class="table-primary"><td class="mono">' + esc(key) + '</td><td>' + esc(name) + '</td><td>' + esc(catalog.room || '') + '</td><td>' + esc(kind) + '</td><td class="mono">' + esc(commands) + '<div class="mt-1">' + action + '</div></td></tr>');
      });
      (model.devices || []).forEach(function(device) {
        if (!device.zone && isAccountCatalogDevice(device)) return;
        const key = device.account + ' / zone ' + device.zone;
        const action = '<button class="btn btn-outline-secondary btn-sm mt-1" data-account="' + esc(device.account) + '" data-zone="' + esc(device.zone) + '" onclick="focusCatalogRow(this.dataset.account, this.dataset.zone)">Edit</button>';
        sia.insertAdjacentHTML('beforeend', '<tr><td class="mono">' + esc(key) + '</td><td>' + esc(device.name) + '</td><td>' + esc(device.room) + '</td><td>' + esc(device.kind) + '</td><td class="mono">' + esc(csv(device.jeedom_command_ids)) + '<div>' + action + '</div></td></tr>');
      });

      const jeedom = document.querySelector('#jeedomTable tbody');
      jeedom.innerHTML = '';
      const devices = model.jeedom_devices || [];
      let linkedDevices = 0;
      let commandTotal = 0;
      let numericCommandTotal = 0;
      let diagnosticTotal = 0;
      devices.forEach(function(device) {
        const commands = commandsForDevice(device);
        const linked = !!String(device.linked_account || '').trim();
        const linkedLabel = linked
          ? (device.linked_zone ? (device.linked_account + ' / zone ' + device.linked_zone) : ('Account ' + device.linked_account))
          : 'Not linked to SIA';
        const numericIDs = numericCommandIDs(device);
        const mergeDisabled = numericIDs.length === 0 ? ' disabled title="No current numeric command IDs"' : '';
        const mergeAction = linked
          ? '<button class="btn btn-outline-primary btn-sm mt-2" data-device-slug="' + esc(device.device_slug) + '" onclick="mergeJeedomCommandIDs(this.dataset.deviceSlug)"' + mergeDisabled + '>Merge ' + numericIDs.length + ' numeric IDs</button>'
          : '';
        const linkBadge = linked
          ? '<span class="badge text-bg-success">Linked</span>'
          : '<span class="badge text-bg-warning">Unlinked</span>';

        linkedDevices += linked ? 1 : 0;
        commandTotal += commands.length;
        numericCommandTotal += numericIDs.length;
        diagnosticTotal += commands.filter(function(command) { return command.entity_category === 'diagnostic'; }).length;

        jeedom.insertAdjacentHTML('beforeend',
          '<tr><td class="mono">' + esc(device.device_slug) + '</td>' +
          '<td>' + esc(device.device) + '</td>' +
          '<td>' + esc(device.jeedom_device_type || device.ha_model || '') + '</td>' +
          '<td><div class="mono">' + esc(linkedLabel) + '</div><div class="mt-1">' + linkBadge + '</div>' + mergeAction + '</td>' +
          '<td>' + renderJeedomCommandRows(device, commands, linked) + '</td></tr>');
      });

      const summary = document.getElementById('matchingSummary');
      summary.innerHTML =
        '<span class="badge text-bg-secondary">' + devices.length + ' Jeedom devices</span>' +
        '<span class="badge text-bg-success">' + linkedDevices + ' linked</span>' +
        '<span class="badge text-bg-warning">' + (devices.length - linkedDevices) + ' unlinked</span>' +
        '<span class="badge text-bg-primary">' + commandTotal + ' commands</span>' +
        '<span class="badge text-bg-info">' + numericCommandTotal + ' numeric IDs</span>' +
        '<span class="badge text-bg-light border text-dark">' + diagnosticTotal + ' diagnostic</span>';
    }

    function commandsForDevice(device) {
      const byID = {};
      Object.entries(device.raw_commands || {}).forEach(function(entry) {
        const command = Object.assign({}, entry[1] || {});
        const commandID = String(command.command_id || entry[0] || '').trim();
        if (!commandID) return;
        command.command_id = commandID;
        byID[commandID] = command;
      });
      (model.jeedom_commands || []).forEach(function(command) {
        if (command.device_slug !== device.device_slug) return;
        const commandID = String(command.command_id || '').trim();
        if (!commandID) return;
        byID[commandID] = Object.assign({}, command, byID[commandID] || {});
      });
      (model.jeedom_actions || []).forEach(function(action) {
        if (action.device_slug !== device.device_slug) return;
        const commandID = String(action.command_id || '').trim();
        if (!commandID) return;
        byID[commandID] = {
          command_id: commandID,
          metric: action.action ? ('control_' + action.action) : '',
          component: 'action',
          name: action.name || action.action || 'Action',
          raw_name: action.raw_name || '',
          type: 'action',
          subtype: action.subtype || '',
          value: action.allowed ? 'allowed' : 'blocked',
          is_action: true,
          allowed: !!action.allowed
        };
      });
      return Object.values(byID).sort(function(left, right) {
        return compareCommandIDs(left.command_id, right.command_id);
      });
    }

    function compareCommandIDs(left, right) {
      const leftID = String(left || '');
      const rightID = String(right || '');
      const leftNumeric = isNumericCommandID(leftID);
      const rightNumeric = isNumericCommandID(rightID);
      if (leftNumeric !== rightNumeric) return leftNumeric ? -1 : 1;
      return leftID.localeCompare(rightID, undefined, {numeric:true, sensitivity:'base'});
    }

    function isNumericCommandID(commandID) {
      return /^\d+$/.test(String(commandID || '').trim());
    }

    function numericCommandIDs(device) {
      return commandsForDevice(device)
        .map(function(command) { return String(command.command_id || '').trim(); })
        .filter(isNumericCommandID)
        .filter(function(commandID, index, values) { return values.indexOf(commandID) === index; })
        .sort(compareCommandIDs);
    }

    function renderJeedomCommandRows(device, commands, linked) {
      if (commands.length === 0) {
        return '<span class="text-body-secondary">No current commands</span>';
      }
      const header =
        '<div class="jeedom-command-row jeedom-command-head">' +
        '<div>ID</div><div>Canonical metric</div><div>HA component / class</div><div>Current value</div><div>Name / raw</div></div>';
      const rows = commands.map(function(command) {
        const commandID = String(command.command_id || '').trim();
        const badges = [];
        if (command.entity_category === 'diagnostic') badges.push('<span class="badge text-bg-secondary">Diagnostic</span>');
        if (command.is_action) badges.push('<span class="badge text-bg-primary">Control action</span>');
        if (command.is_action && !command.allowed) badges.push('<span class="badge text-bg-danger">Blocked</span>');
        if (!linked) badges.push('<span class="badge text-bg-warning">Unlinked</span>');
        if (!isNumericCommandID(commandID)) badges.push('<span class="badge text-bg-light border text-dark">Synthetic ID</span>');
        if (!command.metric || !command.component) badges.push('<span class="badge text-bg-danger">Unmapped</span>');
        if (command.visible) badges.push('<span class="badge text-bg-info">Visible</span>');
        if (command.historized) badges.push('<span class="badge text-bg-primary">Historized</span>');

        const component = command.component || 'Unmapped';
        const deviceClass = command.device_class || '—';
        const type = [command.type, command.subtype].filter(Boolean).join(' / ') || '—';
        const rawName = command.raw_name || '—';
        return '<div class="jeedom-command-row">' +
          '<div class="jeedom-command-cell mono">' + esc(commandID || '—') + '</div>' +
          '<div class="jeedom-command-cell"><span class="mono">' + esc(command.metric || '—') + '</span></div>' +
          '<div class="jeedom-command-cell"><span class="mono">' + esc(component) + '</span><small>Device class: ' + esc(deviceClass) + '</small></div>' +
          '<div class="jeedom-command-cell">' + formatCommandValue(device, command) + '</div>' +
          '<div class="jeedom-command-cell"><strong>' + esc(command.name || 'Unnamed') + '</strong><small>Raw: ' + esc(rawName) + '</small><small>Type: ' + esc(type) + '</small><div class="command-badges">' + badges.join('') + '</div></div>' +
          '</div>';
      }).join('');
      return '<div class="jeedom-command-list">' + header + rows + '</div>';
    }

    function formatCommandValue(device, command) {
      let value = command.value;
      if ((value === null || value === undefined) && command.metric && device.values && Object.prototype.hasOwnProperty.call(device.values, command.metric)) {
        value = device.values[command.metric];
      }
      if (value === null || value === undefined || value === '') {
        return '<span class="text-body-secondary">No value</span>';
      }
      if (typeof value === 'object') value = JSON.stringify(value);
      return '<strong>' + esc(value) + '</strong>' + (command.unit ? ' <span class="text-body-secondary">' + esc(command.unit) + '</span>' : '');
    }

    function mergeJeedomCommandIDs(deviceSlug) {
      const device = (model.jeedom_devices || []).find(function(candidate) { return candidate.device_slug === deviceSlug; });
      if (!device || !String(device.linked_account || '').trim()) {
        return alertMsg('danger', 'This Jeedom device is not linked to a SIA catalog row.');
      }

      // Catalog edits live in the form until Save is pressed. Capture them
      // before re-rendering so merging IDs never discards unsaved changes.
      model.devices = collectDevices();
      const account = String(device.linked_account || '').trim();
      const zone = String(device.linked_zone || '').trim();
      const catalogDevice = (model.devices || []).find(function(candidate) {
        return String(candidate.account || '').trim() === account && String(candidate.zone || '').trim() === zone;
      });
      if (!catalogDevice) {
        return alertMsg('danger', 'No Device Catalog row matches account ' + account + (zone ? ' / zone ' + zone : '') + '.');
      }

      const currentIDs = numericCommandIDs(device);
      if (currentIDs.length === 0) {
        return alertMsg('warning', 'No current numeric Jeedom command IDs were found for ' + (device.device || deviceSlug) + '.');
      }
      const previousIDs = (catalogDevice.jeedom_command_ids || []).map(function(commandID) { return String(commandID || '').trim(); }).filter(Boolean);
      const mergedIDs = previousIDs.concat(currentIDs)
        .filter(isNumericCommandID)
        .filter(function(commandID, index, values) { return values.indexOf(commandID) === index; })
        .sort(compareCommandIDs);
      const added = mergedIDs.filter(function(commandID) { return previousIDs.indexOf(commandID) === -1; }).length;
      const ignored = previousIDs.filter(function(commandID) { return !isNumericCommandID(commandID); }).length;
      catalogDevice.jeedom_command_ids = mergedIDs;

      renderDevices();
      renderMatching();
      focusCatalogRow(account, zone);
      alertMsg('info', 'Catalog row now contains ' + mergedIDs.length + ' numeric Jeedom command IDs (' + added + ' added' + (ignored ? ', ' + ignored + ' nonnumeric ignored' : '') + '). Press Save catalog to persist.');
    }

    function accountRows() {
      const rowsByAccount = {};
      (model.devices || []).forEach(function(device) {
        if (isAccountCatalogDevice(device)) {
          rowsByAccount[device.account] = Object.assign({exists:true}, device);
        }
      });
      (((model.state || {}).accounts) || []).forEach(function(account) {
        if (!rowsByAccount[account.account]) {
          rowsByAccount[account.account] = {
            account: account.account,
            name: 'Ajax account ' + account.account,
            kind: 'Hub',
            exists: false
          };
        }
      });
      return Object.keys(rowsByAccount).sort().map(function(account) {
        return {
          account: account,
          name: rowsByAccount[account].name,
          catalog: rowsByAccount[account]
        };
      });
    }

    function isAccountCatalogDevice(device) {
      const kind = String(device.kind || device.device || device.name || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
      return !!device.account && !device.zone && ['account','ajaxaccount','hub','hub2','hub2plus','hubplus','hubhybrid'].includes(kind);
    }

    function addAccountCatalogRow(account) {
      const device = {
        account: account,
        zone: '',
        device: '',
        name: 'Ajax account ' + account,
        room: '',
        kind: 'Hub',
        events: [],
        jeedom_names: [],
        jeedom_command_ids: []
      };
      model.devices = model.devices || [];
      model.devices.unshift(device);
      renderDevices();
      renderMatching();
      focusCatalogRow(account, '');
    }

    function focusCatalogRow(account, zone) {
      const trigger = document.querySelector('[data-bs-target="#devicesTab"]');
      if (trigger) bootstrap.Tab.getOrCreateInstance(trigger).show();
      setTimeout(function() {
        const rows = Array.from(document.querySelectorAll('#devicesTable tbody tr'));
        const row = rows.find(function(tr) {
          return tr.querySelector('[data-field="account"]').value.trim() === account &&
            tr.querySelector('[data-field="zone"]').value.trim() === zone;
        });
        if (!row) return;
        row.classList.add('table-warning');
        const target = row.querySelector('[data-field="jeedom_command_ids"]');
        if (target) target.focus();
        setTimeout(function() { row.classList.remove('table-warning'); }, 1600);
      }, 120);
    }

    function renderNotifications() {
      const cfg = model.notifications || {enabled:true, channels:[], rules:[]};
      document.getElementById('notificationsEnabled').checked = !!cfg.enabled;
      const channels = document.querySelector('#channelsTable tbody');
      channels.innerHTML = '';
      (cfg.channels || []).forEach(appendChannelRow);
      const rules = document.querySelector('#rulesTable tbody');
      rules.innerHTML = '';
      (cfg.rules || []).forEach(appendRuleRow);
      const history = document.querySelector('#historyTable tbody');
      history.innerHTML = '';
      (model.notification_history || []).forEach(function(row) {
        history.insertAdjacentHTML('beforeend', '<tr><td class="mono">' + esc(row.time) + '</td><td>' + esc(row.rule_name || row.rule_id) + '</td><td>' + esc(row.channel_id) + '</td><td>' + esc(row.device) + '</td><td>' + esc(row.event_kind) + '</td><td>' + esc(row.result) + '</td><td>' + esc(row.message || row.error) + '</td></tr>');
      });
    }

    function appendChannelRow(channel) {
      channel = channel || {id:'', type:'log', url:'', method:'POST', headers:{}};
      const tr = document.createElement('tr');
      tr.innerHTML =
        '<td><input class="form-control form-control-sm" data-field="id" value="' + esc(channel.id) + '"></td>' +
        '<td><select class="form-select form-select-sm" data-field="type">' + options(['log','webhook','ntfy'], channel.type) + '</select></td>' +
        '<td><input class="form-control form-control-sm" data-field="url" value="' + esc(channel.url) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="method" value="' + esc(channel.method || 'POST') + '"></td>' +
        '<td><textarea class="form-control form-control-sm" data-field="headers">' + esc(JSON.stringify(channel.headers || {})) + '</textarea></td>' +
        '<td><button class="btn btn-outline-danger btn-sm" onclick="this.closest(\'tr\').remove()">x</button></td>';
      document.querySelector('#channelsTable tbody').appendChild(tr);
    }

    function addChannelRow() {
      appendChannelRow({id:'', type:'log', method:'POST', headers:{}});
    }

	function appendRuleRow(rule) {
	  rule = rule || {enabled:true, name:'', device_slug:'', metric:'temperature_c', condition:'above', threshold:0, arm_modes:['any'], channels:['log'], cooldown:'30m'};
	  const tr = document.createElement('tr');
	  tr.dataset.id = rule.id || '';
	  tr.innerHTML =
        '<td><input class="form-check-input" type="checkbox" data-field="enabled" ' + (rule.enabled ? 'checked' : '') + '></td>' +
        '<td><input class="form-control form-control-sm" data-field="name" value="' + esc(rule.name) + '"></td>' +
        '<td><select class="form-select form-select-sm" data-field="device_slug">' + deviceOptions(rule.device_slug) + '</select></td>' +
        '<td><select class="form-select form-select-sm" data-field="metric">' + options(notificationMetricOptions(rule.metric), rule.metric) + '</select></td>' +
        '<td><select class="form-select form-select-sm" data-field="condition">' + options(['above','below','changed','changed_to_on','changed_to_off','control','control_on','control_off'], rule.condition) + '</select></td>' +
        '<td><input type="number" step="0.01" class="form-control form-control-sm" data-field="threshold" value="' + esc(rule.threshold || 0) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="arm_modes" value="' + esc(csv(rule.arm_modes || ['any'])) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="channels" value="' + esc(csv(rule.channels || ['log'])) + '"></td>' +
        '<td><input class="form-control form-control-sm" data-field="cooldown" value="' + esc(rule.cooldown || '30m') + '"></td>' +
        '<td><button class="btn btn-outline-danger btn-sm" onclick="this.closest(\'tr\').remove()">x</button></td>';
      document.querySelector('#rulesTable tbody').appendChild(tr);
    }

    function addRuleRow() {
      appendRuleRow();
    }

    function addPresetRules() {
      const devices = model.jeedom_devices || [];
      const first = devices[0] ? devices[0].device_slug : '';
      [
        {enabled:true, name:'Temperature high', device_slug:first, metric:'temperature_c', condition:'above', threshold:35, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Temperature low', device_slug:first, metric:'temperature_c', condition:'below', threshold:5, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Voltage low', device_slug:first, metric:'voltage_v', condition:'below', threshold:200, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Voltage high', device_slug:first, metric:'voltage_v', condition:'above', threshold:245, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Power high', device_slug:first, metric:'power_w', condition:'above', threshold:2000, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Current high', device_slug:first, metric:'current_a', condition:'above', threshold:10, arm_modes:['any'], channels:['log'], cooldown:'30m'},
        {enabled:true, name:'Turned on', device_slug:first, metric:'state', condition:'changed_to_on', threshold:0, arm_modes:['any'], channels:['log'], cooldown:'0s'},
        {enabled:true, name:'Turned off', device_slug:first, metric:'state', condition:'changed_to_off', threshold:0, arm_modes:['any'], channels:['log'], cooldown:'0s'}
      ].forEach(appendRuleRow);
    }

    function collectNotifications() {
      const channels = Array.from(document.querySelectorAll('#channelsTable tbody tr')).map(function(tr) {
        const value = function(field) { return tr.querySelector('[data-field="' + field + '"]').value.trim(); };
        let headers = {};
        const rawHeaders = value('headers');
        if (rawHeaders) headers = JSON.parse(rawHeaders);
        return {id:value('id'), type:value('type'), url:value('url'), method:value('method'), headers:headers};
      }).filter(function(channel) { return channel.id || channel.type || channel.url; });
	  const rules = Array.from(document.querySelectorAll('#rulesTable tbody tr')).map(function(tr) {
	    const value = function(field) { return tr.querySelector('[data-field="' + field + '"]').value.trim(); };
	    return {
	      id: tr.dataset.id || '',
	      enabled: tr.querySelector('[data-field="enabled"]').checked,
          name: value('name'),
          device_slug: value('device_slug'),
          metric: value('metric'),
          condition: value('condition'),
          threshold: parseFloat(value('threshold') || '0'),
          arm_modes: splitCSV(value('arm_modes')),
          channels: splitCSV(value('channels')),
          cooldown: value('cooldown') || '30m'
        };
      }).filter(function(rule) { return rule.name || rule.device_slug; });
      return {enabled:document.getElementById('notificationsEnabled').checked, channels:channels, rules:rules};
    }

    async function saveNotifications() {
      let payload;
      try {
        payload = collectNotifications();
      } catch (err) {
        return alertMsg('danger', 'Invalid notification form: ' + err.message);
      }
      const resp = await fetch('/api/admin/notifications', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(payload)});
      if (!resp.ok) return alertMsg('danger', await resp.text());
      model.notifications = await resp.json();
      renderNotifications();
      alertMsg('success', 'Notifications saved');
    }

    function notificationMetricOptions(selected) {
      const seen = {};
      const values = [];
      const discovered = [];
      const add = function(metric) {
        metric = String(metric || '').trim();
        if (!metric || seen[metric]) return;
        seen[metric] = true;
        values.push(metric);
      };
      const discover = function(command) {
        const metric = String((command || {}).metric || '').trim();
        if (metric && !seen[metric]) discovered.push(metric);
      };

      CORE_NOTIFICATION_METRICS.forEach(add);
      (model.jeedom_commands || []).forEach(discover);
      (model.jeedom_devices || []).forEach(function(device) {
        Object.values(device.raw_commands || {}).forEach(discover);
      });
      discovered.sort().forEach(add);
      add(selected);
      return values;
    }

    function options(values, selected) {
      return values.map(function(value) {
        return '<option value="' + esc(value) + '"' + (value === selected ? ' selected' : '') + '>' + esc(value) + '</option>';
      }).join('');
    }

    function deviceOptions(selected) {
      const seen = {};
      const values = [];
      (model.jeedom_devices || []).forEach(function(device) {
        if (!seen[device.device_slug]) {
          seen[device.device_slug] = true;
          values.push({value:device.device_slug, label:device.device + ' (' + device.device_slug + ')'});
        }
      });
      if (selected && !seen[selected]) values.unshift({value:selected, label:selected});
      return values.map(function(item) {
        return '<option value="' + esc(item.value) + '"' + (item.value === selected ? ' selected' : '') + '>' + esc(item.label) + '</option>';
      }).join('');
    }

    function renderStatus() {
      document.getElementById('devicesPath').textContent = model.devices_path || '';
      document.getElementById('notificationsPath').textContent = model.notifications_path || '';
      const tbody = document.querySelector('#accountsTable tbody');
      tbody.innerHTML = '';
      ((model.state || {}).accounts || []).forEach(function(account) {
        const online = account.online ? '<span class="status-dot status-on"></span>' : '<span class="status-dot status-off"></span>';
        tbody.insertAdjacentHTML('beforeend', '<tr><td class="mono">' + esc(account.account) + '</td><td>' + online + '</td><td>' + esc(account.mode) + '</td><td>' + esc(account.alarm_active) + '</td><td>' + esc(account.tamper_active) + '</td><td>' + esc(account.trouble_active) + '</td><td>' + esc(account.last_event_name) + '</td></tr>');
      });
    }

    loadAll().catch(function(err) { alertMsg('danger', err.message); });
  </script>
</body>
</html>`
