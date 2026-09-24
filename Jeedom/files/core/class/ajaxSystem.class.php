<?php

/* Modified 2026-09-24 by AjaxBridge contributors: expanded device telemetry and state parsing. See ajaxbridge-patch/NOTICE.md and license texts; original Jeedom notices remain applicable. */

/* This file is part of Jeedom.
*
* Jeedom is free software: you can redistribute it and/or modify
* it under the terms of the GNU General Public License as published by
* the Free Software Foundation, either version 3 of the License, or
* (at your option) any later version.
*
* Jeedom is distributed in the hope that it will be useful,
* but WITHOUT ANY WARRANTY; without even the implied warranty of
* MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
* GNU General Public License for more details.
*
* You should have received a copy of the GNU General Public License
* along with Jeedom. If not, see <http://www.gnu.org/licenses/>.
*/

/* * ***************************Includes********************************* */
require_once __DIR__  . '/../../../../core/php/core.inc.php';

class ajaxSystem extends eqLogic {
  /*     * *************************Attributs****************************** */

  /*     * ***********************Methode static*************************** */

  

  public static function templateWidget() {
    $return = array('info' => array('string' => array()));
    $return['info']['string']['state'] = array(
      'template' => 'tmplmultistate',
      'test' => array(
        array('operation' => '#value# == "ARMED"', 'state_light' => '<i class="fas fa-lock"></i>'),
        array('operation' => '#value# == "DISARMED"', 'state_light' => '<i class="fas fa-lock-open"></i>'),
        array('operation' => '#value# == "ARMED_NIGHT_MODE_OFF"', 'state_light' => '<i class="fas fa-lock"></i>'),
        array('operation' => '#value# == "DISARMED_NIGHT_MODE_ON"', 'state_light' => '<i class="fas fa-moon"></i>'),
        array('operation' => '#value# == "DISARMED_NIGHT_MODE_OFF"', 'state_light' => '<i class="fas fa-lock-open"></i>'),
        array('operation' => '#value# == "NIGHT_MODE"', 'state_light' => '<i class="fas fa-moon"></i>'),
        array('operation' => '#value# == "ARMED_NIGHT_MODE_ON"', 'state_light' => '<i class="fas fa-lock"></i>'),
        array('operation' => '#value# == "PANIC"', 'state_light' => '<i class="fas fa-exclamation-circle"></i>')
      )
    );
    return $return;
  }

  public static function request($_path, $_data = null, $_type = 'GET') {
    $url = config::byKey('service::cloud::url') . '/service/ajaxSystem';
    $url .= '?path=' . urlencode(str_replace('{userId}', config::byKey('userId', 'ajaxSystem'), $_path));
    if ($_path != '/login' && $_path != '/refresh') {
      $mc = cache::byKey('ajaxSystem::sessionToken');
      $sessionToken = $mc->getValue();
      if (trim($mc->getValue()) == '') {
        $sessionToken = self::refreshToken();
      }
      $url .= '&session_token=' . $sessionToken;
    }
    if ($_data !== null && $_type == 'GET') {
      $url .= '&options=' . urlencode(json_encode($_data));
    }
    $request_http = new com_http($url);
    $request_http->setHeader(array(
      'Content-Type: application/json',
      'Autorization: ' . sha512(mb_strtolower(config::byKey('market::username')) . ':' . config::byKey('market::password'))
    ));
    log::add('ajaxSystem', 'debug', '[request] ' . $url . ' => ' . json_encode($_data));
    if ($_type == 'POST') {
      $request_http->setPost(json_encode($_data));
    }
    if ($_type == 'PUT') {
      $request_http->setPut(json_encode($_data));
    }
    $return = json_decode($request_http->exec(60, 3), true);
    $return = is_json($return, $return);
    if (isset($return['error']) || isset($return['errors'])) {
      sleep(rand(1,30));
      $return = json_decode($request_http->exec(60, 3), true);
      $return = is_json($return, $return);
    }
    if (isset($return['error']) || isset($return['errors'])) {
      throw new \Exception(__('Erreur lors de la requete à Ajax System : ', __FILE__) . json_encode($return));
    }
    if (isset($return['body'])) {
      return $return['body'];
    }
    return $return;
  }

  public static function start() {
    self::refreshAllData();
  }

  public static function refreshAllData() {
    foreach (eqLogic::byType('ajaxSystem', true) as $eqLogic) {
      try {
        sleep(rand(10, 60));
        $eqLogic->refreshData();
        if ($eqLogic->getCache('failedAjaxRequest', 0) > 0) {
          $eqLogic->setCache('failedAjaxRequest', 0);
        }
      } catch (\Exception $e) {
        $eqLogic->setCache('failedAjaxRequest', $eqLogic->getCache('failedAjaxRequest', 0) + 1);
        if ($eqLogic->getCache('failedAjaxRequest', 0) > 3) {
          log::add('ajaxSystem', 'error', __('Erreur lors de la mise à jour des données de :', __FILE__) . ' ' . $eqLogic->getHumanName() . ' => ' . $e->getMessage(), 'ajaxSystem::failedGetData' . $eqLogic->getId());
        }
      }
    }
  }

  public static function login($_username, $_password) {
    if (trim(network::getNetworkAccess('external')) == '') {
      throw new Exception(__('URL d\'accès externe de votre Jeedom invalide. Merci de la configurer dans Réglage -> Système -> Configuration puis onglet Réseaux'));
    }
    $data = self::request('/login', array(
      'login' => $_username,
      'passwordHash' => $_password,
      'userRole' => 'USER',
      'apikey' => jeedom::getApiKey('ajaxSystem'),
      'url' => network::getNetworkAccess('external')
    ), 'POST');
    log::add('ajaxSystem', 'debug', '[login] ' . json_encode($data));
    config::save('refreshToken', $data['refreshToken'], 'ajaxSystem');
    config::save('userId', $data['userId'], 'ajaxSystem');
    cache::set('ajaxSystem::sessionToken', $data['sessionToken'], 60 * 14);
  }

  public static function refreshToken() {
    $data = self::request('/refresh', array(
      'userId' => config::byKey('userId', 'ajaxSystem'),
      'refreshToken' => config::byKey('refreshToken', 'ajaxSystem')
    ), 'POST');
    log::add('ajaxSystem', 'debug', '[refreshToken] ' . json_encode($data));
    if ($data['refreshToken'] == '') {
      log::add('ajaxSystem', 'debug', '[refreshToken] Empty refresh token, retry in 5s');
      sleep(5);
      $data = self::request('/refresh', array(
        'userId' => config::byKey('userId', 'ajaxSystem'),
        'refreshToken' => config::byKey('refreshToken', 'ajaxSystem')
      ), 'POST');
      log::add('ajaxSystem', 'debug', '[refreshToken] ' . json_encode($data));
    }
    if ($data['refreshToken'] == '') {
      throw new Exception(__('Impossible de mettre à jour les tokens d\'accès, refresh token vide : ', __FILE__) . json_encode($data));
    }
    config::save('refreshToken', $data['refreshToken'], 'ajaxSystem');
    cache::set('ajaxSystem::sessionToken', $data['sessionToken'], 60 * 14);
    return $data['sessionToken'];
  }

  public static function sync() {
    $hubs = self::request('/user/{userId}/hubs');
    log::add('ajaxSystem', 'debug', json_encode($hubs));
    foreach ($hubs as $hub) {
      $hub_info = self::request('/user/{userId}/hubs/' . $hub['hubId']);
      log::add('ajaxSystem', 'debug', json_encode($hub_info));
      $eqLogic = eqLogic::byLogicalId($hub['hubId'], 'ajaxSystem');
      if (!is_object($eqLogic)) {
        $eqLogic = new ajaxSystem();
        $eqLogic->setEqType_name('ajaxSystem');
        $eqLogic->setIsEnable(1);
        $eqLogic->setName($hub_info['name']);
        $eqLogic->setCategory('security', 1);
        $eqLogic->setIsVisible(1);
      }
      $eqLogic->setConfiguration('type', 'hub');
      $eqLogic->setConfiguration('color', $hub_info['color']);
      $eqLogic->setConfiguration('device', $hub_info['hubSubtype']);
      $eqLogic->setConfiguration('ip', $hub_info['ethernet']['ip']);
      $eqLogic->setConfiguration('firmware', $hub_info['firmware']['version']);
      $eqLogic->setLogicalId($hub['hubId']);
      $eqLogic->save();
      $eqLogic->ensureInfoCommands();
      $eqLogic->refreshData();

      $devices = self::request('/user/{userId}/hubs/' . $hub['hubId'] . '/devices');
      log::add('ajaxSystem', 'debug', json_encode($devices));
      foreach ($devices as $device) {
        if (!isset($device['id'])) {
              continue;
        }
        $eqLogic = eqLogic::byLogicalId($device['id'], 'ajaxSystem');
        $device_info = self::request('/user/{userId}/hubs/' . $hub['hubId'] . '/devices/' . $device['id']);
        usleep(200000);
        if(!isset($device_info['deviceName']) || $device_info['deviceName'] == ''){
          continue;
        }
        if (!is_object($eqLogic)) {
          $eqLogic = new ajaxSystem();
          $eqLogic->setEqType_name('ajaxSystem');
          $eqLogic->setIsEnable(1);
          $eqLogic->setName($device_info['deviceName']);
          $eqLogic->setCategory('security', 1);
          $eqLogic->setIsVisible(1);
        }
        $eqLogic->setConfiguration('hub_id', $hub['hubId']);
        $eqLogic->setConfiguration('type', 'device');
        $eqLogic->setConfiguration('color', $device_info['color']);
        $eqLogic->setConfiguration('device', $device_info['deviceType']);
        $eqLogic->setConfiguration('firmware', $device_info['firmwareVersion']);
        $eqLogic->setLogicalId($device['id']);
        $eqLogic->save();
        $eqLogic->ensureInfoCommands();
        $eqLogic->refreshData($device_info);
      }

      $groups = self::request('/user/{userId}/hubs/' . $hub['hubId'] . '/groups');
      log::add('ajaxSystem', 'debug', json_encode($groups));
      foreach ($groups as $group) {
        if($group['groupName'] == ''){
          continue;
        }
        $eqLogic = eqLogic::byLogicalId($group['id'], 'ajaxSystem');
        if (!is_object($eqLogic)) {
          $eqLogic = new ajaxSystem();
          $eqLogic->setEqType_name('ajaxSystem');
          $eqLogic->setIsEnable(1);
          $eqLogic->setName($group['groupName']);
          $eqLogic->setCategory('security', 1);
          $eqLogic->setIsVisible(1);
        }
        $eqLogic->setConfiguration('hub_id', $hub['hubId']);
        $eqLogic->setConfiguration('type', 'group');
        $eqLogic->setConfiguration('device', 'group');
        $eqLogic->setLogicalId($group['id']);
        $eqLogic->save();
        $eqLogic->refreshData($group);
      }
    }
  }


  public static function devicesParameters($_device = '') {
    $return = array();
    $files = ls(__DIR__ . '/../config/devices', '*.json', false, array('files', 'quiet'));
    foreach ($files as $file) {
      try {
        $return[str_replace('.json', '', $file)] = is_json(file_get_contents(__DIR__ . '/../config/devices/' . $file), false);
      } catch (Exception $e) {
      }
    }
    if (isset($_device) && $_device != '') {
      if (isset($return[$_device])) {
        return $return[$_device];
      }
      return array();
    }
    return $return;
  }

  /*     * *********************Méthodes d'instance************************* */

  public function postSave() {
    if ($this->getConfiguration('applyDevice') != $this->getConfiguration('device')) {
      $this->applyModuleConfiguration();
    }
    foreach ($this->getCmd() as $cmd) {
      if(strpos($cmd->getLogicalId(),'sia') !== false){
        $cmd->remove();
      }
    }
  }

  public function applyModuleConfiguration() {
    $this->setConfiguration('applyDevice', $this->getConfiguration('device'));
    $this->save();
    if ($this->getConfiguration('device') == '') {
      return true;
    }
    $device = self::devicesParameters($this->getConfiguration('device'));
    if (!is_array($device)) {
      return true;
    }
    $this->import($device, true);
  }

  // Add new telemetry without overwriting command IDs, history or user settings.
  // Unlike import(), match by logical ID only: a custom command may share a name.
  public function ensureInfoCommands() {
    $device = self::devicesParameters($this->getConfiguration('device'));
    if (!isset($device['commands']) || !is_array($device['commands'])) {
      return;
    }
    $logicalIds = array();
    $names = array();
    $order = 0;
    foreach ($this->getCmd() as $cmd) {
      $logicalIds[$cmd->getLogicalId()] = true;
      $names[$cmd->getName()] = true;
      $order = max($order, $cmd->getOrder() + 1);
    }
    foreach ($device['commands'] as $definition) {
      if ($definition['type'] != 'info' || !isset($definition['logicalId']) || isset($logicalIds[$definition['logicalId']])) {
        continue;
      }
      $name = $definition['name'];
      if (isset($names[$name])) {
        $name .= ' (' . $definition['logicalId'] . ')';
      }
      $uniqueName = $name;
      $suffix = 2;
      while (isset($names[$uniqueName])) {
        $uniqueName = $name . ' ' . $suffix++;
      }
      $definition['name'] = $uniqueName;
      $cmd = new ajaxSystemCmd();
      utils::a2o($cmd, $definition);
      $cmd->setEqLogic_id($this->getId());
      $cmd->setOrder($order++);
      $cmd->setConfiguration('logicalId', $cmd->getLogicalId());
      $cmd->save();
      $logicalIds[$cmd->getLogicalId()] = true;
      $names[$cmd->getName()] = true;
    }
  }

  public function getImage() {
    if(method_exists($this,'getCustomImage')){
         $customImage = $this->getCustomImage();
         if($customImage !== null){
            return $customImage;
         }
      }
    if (file_exists(__DIR__ . '/../config/devices/' .  $this->getConfiguration('device') . '_' . strtolower($this->getConfiguration('color')) . '.png')) {
      return 'plugins/ajaxSystem/core/config/devices/' .  $this->getConfiguration('device') . '_' . strtolower($this->getConfiguration('color')) . '.png';
    }
    if (file_exists(__DIR__ . '/../config/devices/' .  $this->getConfiguration('device') . '.png')) {
      return 'plugins/ajaxSystem/core/config/devices/' .  $this->getConfiguration('device') . '.png';
    }
    return false;
  }

  public function refreshData($_data = null) {
    if($_data === null){
      $datas = array();
      if ($this->getConfiguration('type') == 'hub') {
        $datas = self::request('/user/{userId}/hubs/' . $this->getLogicalId());
      }
      if ($this->getConfiguration('type') == 'device') {
        $datas = self::request('/user/{userId}/hubs/' . $this->getConfiguration('hub_id') . '/devices/' . $this->getLogicalId());
      }
    }else{
      $datas = $_data;
    }
    if (isset($datas['firmwareVersion']) && $datas['firmwareVersion'] != $this->getConfiguration('firmware')) {
      $this->setConfiguration('firmware', $datas['firmwareVersion']);
      $this->save(true);
    }
    $this->updateData($datas);
  }

  // Consume a snapshot or a partial callback. This method never fetches data.
  public function updateData($_data, $_isUpdate = false) {
    if (!is_array($_data)) {
      return;
    }
    if ($_isUpdate) {
      if (in_array($this->getConfiguration('type'), array('hub', 'group')) && isset($_data['state'])) {
        $states = array(0 => 'DISARMED', 1 => 'ARMED', 2 => 'NIGHT_MODE');
        if (isset($states[$_data['state']])) {
          $_data['state'] = $states[$_data['state']];
        }
      }
      if (isset($_data['hubPowered'])) {
        $_data['externallyPowered'] = $_data['hubPowered'];
      }
      if (isset($_data['realState'])) {
        $_data['realState'] = ($_data['realState'] == 0) ? 1 : 0;
      }
      if (in_array($this->getConfiguration('device'), array('LightSwitchTwoChannelTwoWay', 'LightSwitchTwoGang')) && isset($_data['channelStatus'])) {
        $channels = $_data['channelStatus'];
        if (is_numeric($channels) && in_array($channels, array(0, 1, 2, 3))) {
          $_data['channelStatus_1'] = ((int) $channels & 1) ? 1 : 0;
          $_data['channelStatus_2'] = ((int) $channels & 2) ? 1 : 0;
        }
      }
    }

    // The API reports switchState flags; older proxy callbacks report realState.
    // Normalize AFTER callback inversion so the API state is not inverted twice.
    if (in_array($this->getConfiguration('device'), array('WallSwitch', 'Relay'), true) && isset($_data['switchState']) && is_array($_data['switchState'])) {
      $switchedOn = in_array('SWITCHED_ON', $_data['switchState'], true);
      $switchedOff = false;
      $offStates = array('SWITCHED_OFF', 'OFF_TOO_LOW_VOLTAGE', 'OFF_HIGH_VOLTAGE', 'OFF_HIGH_TEMPERATURE');
      if ($this->getConfiguration('device') == 'WallSwitch') {
        $offStates[] = 'OFF_HIGH_CURRENT';
        $offStates[] = 'OFF_SHORT_CIRCUIT';
      }
      foreach ($offStates as $offState) {
        if (in_array($offState, $_data['switchState'], true)) {
          $switchedOff = true;
          break;
        }
      }
      // CONTACT_HANG alone, empty data and conflicting flags do not imply on/off.
      if ($switchedOn !== $switchedOff) {
        $_data['realState'] = $switchedOn ? 1 : 0;
      }
    }

    // Ajax snapshots and the Jeedom callback use different battery field names.
    $batteryChargeLevel = null;
    foreach (array('batteryCharge', 'batteryChargeLevelPercentage', 'battery::chargeLevelPercentage') as $key) {
      if (isset($_data[$key])) {
        $batteryChargeLevel = $_data[$key];
      }
    }
    if (isset($_data['battery']['chargeLevelPercentage'])) {
      $batteryChargeLevel = $_data['battery']['chargeLevelPercentage'];
    }
    $validBattery = is_numeric($batteryChargeLevel) && $batteryChargeLevel >= 0 && $batteryChargeLevel <= 100;
    if ($validBattery) {
      $_data['batteryChargeLevelPercentage'] = $batteryChargeLevel;
      $_data['battery::chargeLevelPercentage'] = $batteryChargeLevel;
      $_data['batteryCharge'] = $batteryChargeLevel;
    }

    foreach ($this->getCmd('info') as $cmd) {
      $logicalId = $cmd->getLogicalId();
      if (in_array($logicalId, array('batteryCharge', 'batteryChargeLevelPercentage', 'battery::chargeLevelPercentage')) && !$validBattery) {
        continue;
      }
      if (isset($_data[$logicalId])) {
        $value = $_data[$logicalId];
      } else {
        $value = $_data;
        foreach (explode('::', $logicalId) as $key) {
          if (!is_array($value) || !isset($value[$key])) {
            continue 2;
          }
          $value = $value[$key];
        }
      }
      if (is_array($value)) {
        if ($cmd->getSubType() != 'string') {
          continue;
        }
        $value = json_encode($value);
      }
      $this->checkAndUpdateCmd($cmd, $value);
    }
    if ($validBattery) {
      $this->batteryStatus($batteryChargeLevel);
    }
    if ($_isUpdate && $this->getConfiguration('device') == 'Socket') {
      $current = $this->getCmd('info', 'currentMA');
      $voltage = $this->getCmd('info', 'voltage');
      if (is_object($current) && is_object($voltage)) {
        $this->checkAndUpdateCmd('power', $current->execCmd() * $voltage->execCmd());
      }
    }
  }

  /*     * **********************Getteur Setteur*************************** */
}

class ajaxSystemCmd extends cmd {
  /*     * *************************Attributs****************************** */


  /*     * ***********************Methode static*************************** */


  /*     * *********************Methode d'instance************************* */


  public function alreadyInState($_options) {
    $eqLogic = $this->getEqLogic();
    if ($eqLogic->getConfiguration('type') == 'hub') {
      $cmdValue = $this->getCmdValue();
      $value =  $cmdValue->execCmd();
      if ($this->getLogicalId() == 'ARM') {
        return ($value == 'ARMED');
      }
      if ($this->getLogicalId() == 'DISARM') {
        return ($value == 'DISARMED_NIGHT_MODE_OFF' || $value == 'DISARMED');
      }
      if ($this->getLogicalId() == 'NIGHT_MODE') {
        return ($value == 'NIGHT_MODE');
      }
      if ($this->getLogicalId() == 'PANIC' || $this->getLogicalId() == 'muteFireDetectors') {
        return false;
      }
    }
    if ($eqLogic->getConfiguration('type') == 'group') {
      return false;
    }
    return parent::alreadyInState($_options);
  }

  public function execute($_options = array()) {
    $eqLogic = $this->getEqLogic();
    if ($eqLogic->getConfiguration('type') == 'hub') {
      if ($this->getLogicalId() == 'ARM') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getLogicalId() . '/commands/arming', array('command' => 'ARM', 'ignoreProblems' => true), 'PUT');
      } else if ($this->getLogicalId() == 'DISARM') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getLogicalId() . '/commands/arming', array('command' => 'DISARM', 'ignoreProblems' => true), 'PUT');
      } else if ($this->getLogicalId() == 'NIGHT_MODE') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getLogicalId() . '/commands/arming', array('command' => 'NIGHT_MODE_ON', 'ignoreProblems' => true), 'PUT');
      } else if ($this->getLogicalId() == 'PANIC') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getLogicalId() . '/commands/panic', array('location' => array('latitude' => 0, 'longitude' => 0, 'accuracy' => 0, 'speed' => 0, 'timestamp' => 0)), 'PUT');
      } else if ($this->getLogicalId() == 'muteFireDetectors') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getLogicalId() . '/commands/muteFireDetectors', array('muteType' => 'ALL_FIRE_DETECTORS'), 'PUT');
      }
    } else if ($eqLogic->getConfiguration('type') == 'device') {
      $command = array(
        'command' => explode(" ", $this->getLogicalId())[0],
        'deviceType' => $eqLogic->getConfiguration('device')
      );
      if($this->getConfiguration('AdditionalParam',null) != null){
        $command['AdditionalParam'] = $this->getConfiguration('AdditionalParam',null);
      }
      if(!isset($command['command']) || $command['command'] == ''){
        $command['command'] = $this->getLogicalId();
      }
      if(isset(explode(" ", $this->getLogicalId())[1]) && explode(" ", $this->getLogicalId())[1] != ""){
          $command['additionalParam'] = array(
            'additionalParamType' => 'CHANNELS',
            'channels' => array("CHANNEL_".explode(" ", $this->getLogicalId())[1])
          );
      }
      log::add('ajaxSystem','debug','Command send to ajax : '.json_encode($command));
      ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getConfiguration('hub_id') . '/devices/' . $eqLogic->getLogicalId() . '/command', $command, 'POST');
    } else if ($eqLogic->getConfiguration('type') == 'group') {
      if ($this->getLogicalId() == 'ARM') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getConfiguration('hub_id') . '/groups/' . $eqLogic->getLogicalId()  . '/commands/arming', array('command' => 'ARM', 'ignoreProblems' => true), 'PUT');
      } else if ($this->getLogicalId() == 'DISARM') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getConfiguration('hub_id') . '/groups/' . $eqLogic->getLogicalId() . '/commands/arming', array('command' => 'DISARM', 'ignoreProblems' => true), 'PUT');
      } else if ($this->getLogicalId() == 'NIGHT_MODE') {
        ajaxSystem::request('/user/{userId}/hubs/' . $eqLogic->getConfiguration('hub_id') . '/groups/' . $eqLogic->getLogicalId() . '/commands/arming', array('command' => 'NIGHT_MODE_ON', 'ignoreProblems' => true), 'PUT');
      }
    }
  }

  /*     * **********************Getteur Setteur*************************** */
}
