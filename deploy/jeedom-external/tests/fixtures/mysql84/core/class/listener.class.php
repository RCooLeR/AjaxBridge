<?php
// Minimal parser-contract fixture. These methods are never executed.
class listener {
    public static function clean() {
        $sql = 'SELECT ' . DB::buildField(__CLASS__) . '
                FROM listener GROUP BY class, function, event, option
                HAVING count(*) > 1';
        $duplicateds = DB::Prepare($sql, array(), DB::FETCH_TYPE_ALL, PDO::FETCH_CLASS, __CLASS__);
        return $duplicateds;
    }
    public static function check($_event, $_value) {
        // Sentinel outside the patched method; it must stay byte-identical.
        return [$_event, $_value];
    }
}
