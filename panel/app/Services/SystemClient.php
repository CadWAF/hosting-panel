<?php

namespace App\Services;

/** Client for the System Service: monitoring, software, security, updates, scheduled jobs, logs, Docker, FTP. */
class SystemClient extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.system_socket');
    }

    private function seg(string $v): string
    {
        return rawurlencode($v);
    }

    public function stats(): array
    {
        return $this->call('GET', '/v1/stats')['data'] ?? [];
    }

    // Software store and systemd units
    public function software(): array
    {
        $r = $this->call('GET', '/v1/software');

        return ['items' => $r['data'] ?? [], 'categories' => $r['extra']['categories'] ?? []];
    }

    public function installSoftware(string $id): array
    {
        return $this->call('POST', '/v1/software/'.$this->seg($id).'/install');
    }

    public function removeSoftware(string $id): array
    {
        return $this->call('POST', '/v1/software/'.$this->seg($id).'/remove');
    }

    public function services(bool $all = false): array
    {
        return $this->call('GET', '/v1/services'.($all ? '?all=1' : ''))['data'] ?? [];
    }

    public function serviceAction(string $unit, string $action): array
    {
        return $this->call('POST', '/v1/services/'.$this->seg($unit).'/'.$this->seg($action));
    }

    // Security
    public function fail2ban(): array
    {
        return $this->call('GET', '/v1/fail2ban')['data'] ?? [];
    }

    public function setJail(string $name, array $in): array
    {
        return $this->call('PUT', '/v1/fail2ban/jails/'.$this->seg($name), $in);
    }

    public function setF2bDefaults(array $in): array
    {
        return $this->call('PUT', '/v1/fail2ban/defaults', $in);
    }

    public function ban(string $jail, string $ip): array
    {
        return $this->call('POST', '/v1/fail2ban/jails/'.$this->seg($jail).'/ban', ['ip' => $ip]);
    }

    public function unban(string $jail, string $ip): array
    {
        return $this->call('DELETE', '/v1/fail2ban/jails/'.$this->seg($jail).'/ban/'.$this->seg($ip));
    }

    public function firewall(): array
    {
        return $this->call('GET', '/v1/firewall')['data'] ?? [];
    }

    public function enableFirewall(): array
    {
        return $this->call('POST', '/v1/firewall/enable');
    }

    public function disableFirewall(): array
    {
        return $this->call('POST', '/v1/firewall/disable');
    }

    public function addRule(array $in): array
    {
        return $this->call('POST', '/v1/firewall/rules', $in);
    }

    public function deleteRule(int $n, string $spec): array
    {
        return $this->call('POST', '/v1/firewall/rules/delete', ['n' => $n, 'spec' => $spec]);
    }

    public function ssh(): array
    {
        return $this->call('GET', '/v1/ssh')['data'] ?? [];
    }

    public function setSsh(array $in): array
    {
        return $this->call('PUT', '/v1/ssh', $in);
    }

    // Updates
    public function updates(): array
    {
        return $this->call('GET', '/v1/updates')['data'] ?? [];
    }

    public function checkUpdates(): array
    {
        return $this->call('POST', '/v1/updates/check');
    }

    public function applyUpdates(): array
    {
        return $this->call('POST', '/v1/updates/apply');
    }

    public function reboot(): array
    {
        return $this->call('POST', '/v1/reboot');
    }

    // Scheduled jobs
    public function cron(): array
    {
        return $this->call('GET', '/v1/cron')['data'] ?? [];
    }

    public function addCron(array $job): array
    {
        return $this->call('POST', '/v1/cron', $job);
    }

    public function updateCron(string $id, array $job): array
    {
        return $this->call('PUT', '/v1/cron/'.$this->seg($id), $job);
    }

    public function deleteCron(string $id): array
    {
        return $this->call('DELETE', '/v1/cron/'.$this->seg($id));
    }

    public function runCron(string $id): array
    {
        return $this->call('POST', '/v1/cron/'.$this->seg($id).'/run');
    }

    // Logs
    public function logs(): array
    {
        return $this->call('GET', '/v1/logs')['data'] ?? [];
    }

    public function readLog(string $id, int $lines = 300, string $q = ''): array
    {
        return $this->call('GET', '/v1/logs/read?'.http_build_query(['id' => $id, 'lines' => $lines, 'q' => $q]));
    }

    // Docker
    public function cadwaf(): array
    {
        return $this->call('GET', '/v1/cadwaf')['data'] ?? [];
    }

    public function installCadwaf(string $allowIp = ''): array
    {
        return $this->call('POST', '/v1/cadwaf/install', ['allow_ip' => $allowIp]);
    }

    public function cadwafRelease(bool $refresh = false): array
    {
        return $this->call('GET', '/v1/cadwaf/release'.($refresh ? '?refresh=1' : ''))['data'] ?? [];
    }

    public function updateCadwaf(): array
    {
        return $this->call('POST', '/v1/cadwaf/update');
    }

    public function removeCadwaf(): array
    {
        return $this->call('POST', '/v1/cadwaf/remove');
    }

    public function setCadwafPorts(array $in): array
    {
        $in = ['tcp_in' => [], 'udp_in' => [], 'tcp_out' => [], 'udp_out' => [], 'allow_localhost' => true] + $in;

        return $this->call('PUT', '/v1/cadwaf/ports', $in);
    }

    public function addCadwafIp(string $list, string $ip): array
    {
        return $this->call('POST', '/v1/cadwaf/ips/'.$this->seg($list), ['ip' => $ip]);
    }

    public function deleteCadwafIp(string $list, string $ip): array
    {
        return $this->call('POST', '/v1/cadwaf/ips/'.$this->seg($list).'/delete', ['ip' => $ip]);
    }

    public function addCadwafRule(string $rule, string $file): array
    {
        return $this->call('POST', '/v1/cadwaf/rules', ['rule' => $rule, 'file' => $file]);
    }

    public function deleteCadwafRule(int $id, string $file): array
    {
        return $this->call('POST', '/v1/cadwaf/rules/delete', ['id' => $id, 'file' => $file]);
    }

    public function reloadCadwaf(): array
    {
        return $this->call('POST', '/v1/cadwaf/reload');
    }

    public function setCadwafEgress(bool $on): array
    {
        return $this->call('POST', '/v1/cadwaf/egress', ['enabled' => $on]);
    }

    public function cadwafService(string $action): array
    {
        return $this->call('POST', '/v1/cadwaf/service/'.$this->seg($action));
    }

    public function docker(): array
    {
        return $this->call('GET', '/v1/docker')['data'] ?? [];
    }

    public function runContainer(array $in): array
    {
        return $this->call('POST', '/v1/docker/containers', $in);
    }

    public function containerAction(string $id, string $action): array
    {
        return $this->call('POST', '/v1/docker/containers/'.$this->seg($id).'/'.$this->seg($action));
    }

    public function containerLog(string $id, int $lines = 200): array
    {
        return $this->call('GET', '/v1/docker/containers/'.$this->seg($id).'/logs?lines='.$lines);
    }

    public function pullImage(string $image): array
    {
        return $this->call('POST', '/v1/docker/images/pull', ['image' => $image]);
    }

    public function removeImage(string $id): array
    {
        return $this->call('DELETE', '/v1/docker/images/'.$this->seg($id));
    }

    // FTP
    public function ftp(): array
    {
        return $this->call('GET', '/v1/ftp')['data'] ?? [];
    }

    public function addFtp(array $in): array
    {
        return $this->call('POST', '/v1/ftp/accounts', $in);
    }

    public function setFtpPassword(string $name, string $password): array
    {
        return $this->call('PUT', '/v1/ftp/accounts/'.$this->seg($name).'/password', ['password' => $password]);
    }

    public function deleteFtp(string $name): array
    {
        return $this->call('DELETE', '/v1/ftp/accounts/'.$this->seg($name));
    }

    public function openFtpPorts(): array
    {
        return $this->call('POST', '/v1/ftp/open-ports');
    }

    public function task(string $id): ?array
    {
        return $this->call('GET', '/v1/tasks/'.$this->seg($id))['data'];
    }

    public function tasks(): array
    {
        return $this->call('GET', '/v1/tasks')['data'] ?? [];
    }
}
