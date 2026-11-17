<?php

namespace App\Services;

/** Client for the Core Agent. */
class CoreAgent extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.core_socket');
    }

    public function sites(): array
    {
        return $this->call('GET', '/v1/sites')['data'] ?? [];
    }

    public function createSite(string $domain, string $php, string $mode = 'nginx', string $preset = 'medium'): array
    {
        return $this->call('POST', '/v1/sites', ['domain' => $domain, 'php_version' => $php, 'web_mode' => $mode, 'preset' => $preset]);
    }

    public function updateSite(string $domain, array $changes): array
    {
        if (array_key_exists('php_settings', $changes)) {
            $changes['php_settings'] = (object) $changes['php_settings'];
        }

        return $this->call('PATCH', '/v1/sites/'.rawurlencode($domain), $changes);
    }

    public function setSiteEnabled(string $domain, bool $enabled): array
    {
        return $this->call('POST', '/v1/sites/'.rawurlencode($domain).'/'.($enabled ? 'enable' : 'disable'));
    }

    public function deleteSite(string $domain): array
    {
        return $this->call('DELETE', '/v1/sites/'.rawurlencode($domain));
    }

    public function php(): array
    {
        $r = $this->call('GET', '/v1/php');

        return ['runtimes' => $r['data'] ?? [], 'extensions' => $r['extra']['extensions'] ?? []];
    }

    public function settingKeys(): array
    {
        return $this->call('GET', '/v1/php/settings-keys')['data'] ?? [];
    }

    public function installPhp(string $v): array
    {
        return $this->call('POST', "/v1/php/$v/install");
    }

    public function removePhp(string $v): array
    {
        return $this->call('DELETE', "/v1/php/$v");
    }

    public function installExtension(string $v, string $ext): array
    {
        return $this->call('POST', "/v1/php/$v/extensions/$ext");
    }

    public function removeExtension(string $v, string $ext): array
    {
        return $this->call('DELETE', "/v1/php/$v/extensions/$ext");
    }

    public function installApache(): array
    {
        return $this->call('POST', '/v1/apache/install');
    }

    public function site(string $d): array
    {
        return $this->call('GET', '/v1/sites/'.rawurlencode($d))['data'] ?? [];
    }

    public function issueTls(string $d, bool $www, string $email): array
    {
        return $this->call('POST', '/v1/sites/'.rawurlencode($d).'/tls', ['www' => $www, 'email' => $email]);
    }

    public function removeTls(string $d): array
    {
        return $this->call('DELETE', '/v1/sites/'.rawurlencode($d).'/tls');
    }

    public function tasks(): array
    {
        return $this->call('GET', '/v1/tasks')['data'] ?? [];
    }

    public function task(string $id): ?array
    {
        return $this->call('GET', '/v1/tasks/'.rawurlencode($id))['data'];
    }

    private function sitePath(string $d): string
    {
        return '/v1/sites/'.rawurlencode($d);
    }

    public function sshKeys(string $d): array
    {
        return $this->call('GET', $this->sitePath($d).'/ssh-keys')['data'] ?? [];
    }

    public function addSshKey(string $d, string $label, string $public): array
    {
        return $this->call('POST', $this->sitePath($d).'/ssh-keys', ['label' => $label, 'public_key' => $public]);
    }

    public function generateSshKey(string $d, string $label): array
    {
        return $this->call('POST', $this->sitePath($d).'/ssh-keys', ['label' => $label, 'generate' => true]);
    }

    public function removeSshKey(string $d, int $id): array
    {
        return $this->call('DELETE', $this->sitePath($d)."/ssh-keys/$id");
    }

    public function cron(string $d): array
    {
        return $this->call('GET', $this->sitePath($d).'/cron')['data'] ?? [];
    }

    public function addCron(string $d, string $schedule, string $command): array
    {
        return $this->call('POST', $this->sitePath($d).'/cron', ['schedule' => $schedule, 'command' => $command]);
    }

    public function removeCron(string $d, int $id): array
    {
        return $this->call('DELETE', $this->sitePath($d)."/cron/$id");
    }

    public function log(string $d, string $name, int $lines = 200): string
    {
        return (string) ($this->call('GET', $this->sitePath($d)."/logs/$name?lines=$lines")['data'] ?? '');
    }

    public function files(string $d, string $path): array
    {
        return $this->call('GET', $this->sitePath($d).'/files?path='.rawurlencode($path));
    }

    public function readFile(string $d, string $path): array
    {
        return $this->call('GET', $this->sitePath($d).'/files/content?path='.rawurlencode($path));
    }

    public function writeFile(string $d, string $path, string $content): array
    {
        return $this->putRaw($this->sitePath($d).'/files/content?path='.rawurlencode($path), $content);
    }

    public function uploadFile(string $d, string $path, $stream): array
    {
        return $this->putRaw($this->sitePath($d).'/files/upload?path='.rawurlencode($path), $stream);
    }

    public function fileOp(string $d, string $op, array $args): array
    {
        return $this->call('POST', $this->sitePath($d)."/files/$op", $args);
    }

    public function download(string $d, string $path)
    {
        return $this->stream('GET', $this->sitePath($d).'/files/download?path='.rawurlencode($path));
    }

    public function compress(string $d, array $paths)
    {
        return $this->stream('POST', $this->sitePath($d).'/files/compress', ['paths' => $paths]);
    }
}
