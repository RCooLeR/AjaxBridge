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

require_once dirname(__FILE__) . '/../../../core/php/core.inc.php';

// Fonction exécutée automatiquement après l'installation du plugin
function ajaxSystem_install() {
  
}

// Fonction exécutée automatiquement après la mise à jour du plugin
function ajaxSystem_update() {
  $cron = cron::byClassAndFunction('ajaxSystem', 'refreshAllData');
  if (is_object($cron)) {
    $cron->remove();
  }
  foreach (eqLogic::byType('ajaxSystem') as $eqLogic) {
    $eqLogic->ensureInfoCommands();
    $cmd = $eqLogic->getCmd('action', 'refresh');
    if (is_object($cmd)) {
      $cmd->remove();
    }
  }
}

// Fonction exécutée automatiquement après la suppression du plugin
function ajaxSystem_remove() {
  $cron = cron::byClassAndFunction('ajaxSystem', 'refreshAllData');
  if (is_object($cron)) {
    $cron->remove();
  }
}
