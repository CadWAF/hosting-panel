<?php

return [
    // Unix socket of the Core Agent.
    'core_socket' => env('PANEL_CORE_SOCKET', '/run/hostingpanel/core.sock'),
    'db_socket' => env('PANEL_DB_SOCKET', '/run/hostingpanel/db.sock'),
    'backup_socket' => env('PANEL_BACKUP_SOCKET', '/run/hostingpanel/backup.sock'),
    'mail_socket' => env('PANEL_MAIL_SOCKET', '/run/hostingpanel/mail.sock'),
    'system_socket' => env('PANEL_SYSTEM_SOCKET', '/run/hostingpanel/system.sock'),
    'notifier_socket' => env('PANEL_NOTIFIER_SOCKET', '/run/hostingpanel/notifier.sock'),
    'default_php' => env('PANEL_DEFAULT_PHP', '8.3'),
    'php_versions' => ['8.1', '8.2', '8.3', '8.4'],
];
