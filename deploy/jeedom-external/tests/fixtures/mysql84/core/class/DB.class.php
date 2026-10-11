<?php
// Minimal parser-contract fixture, not a runnable Jeedom database implementation.
// Real public Jeedom classes are exercised separately by the local MySQL smoke.
class DB {
    private static $connection;
    public static function initConnection() {
        $options = [PDO::MYSQL_ATTR_INIT_COMMAND => 'SET NAMES utf8mb4'];
        if (getenv('FIXTURE_ALTERNATE_CONNECTION')) {
            static::$connection = new PDO('mysql:host=fixture', 'fixture', '', $options);
        } else {
            static::$connection = new PDO('mysql:host=fixture', 'fixture', '', $options);
        }
    }
    public static function buildDefinitionIndex($_index, $_table = '') {
        $return = 'CREATE FULLTEXT INDEX fixture ON fixture (';
        foreach ($_index['columns'] as $column) {
            $return .= $column['column'];
            $return .= ' ASC,';
        }
        return $return;
    }
    public static function compareField($_ref_field, $_real_field) {
        if ($_ref_field['type'] != $_real_field['Type']) {
            return static::buildDefinitionField($_ref_field);
        }
        return [];
    }
}
