<?php

namespace App\Http\Controllers;

use App\Services\BackupClient;
use App\Services\CoreAgent;
use App\Services\CoreAgentUnavailable;
use App\Services\DbManagerClient;
use App\Services\MailClient;
use App\Services\NotifierClient;
use App\Services\SystemClient;
use Illuminate\Http\Request;
use Inertia\Inertia;
use Inertia\Response;

/** Pages only read from the Services; every change goes through RpcController. A dead Service never breaks a page. */
class PageController extends Controller
{
    /** @param callable():array $fn */
    private function load(callable $fn, array $empty): array
    {
        try {
            return $fn() + ['available' => true];
        } catch (CoreAgentUnavailable) {
            return $empty + ['available' => false];
        }
    }

    public function sites(CoreAgent $core): Response
    {
        $d = $this->load(function () use ($core) {
            $installed = collect($core->php()['runtimes'] ?? [])->where('installed', true)->pluck('version')->values()->all();
            $keys = $core->settingKeys();
            sort($keys);

            return ['sites' => $core->sites(), 'installed' => $installed, 'settingKeys' => $keys];
        }, ['sites' => [], 'installed' => [], 'settingKeys' => []]);

        return Inertia::render('Sites', $d + ['phpVersions' => config('panel.php_versions')]);
    }

    public function site(string $domain, CoreAgent $core): Response
    {
        $d = $this->load(fn () => ['site' => $core->site($domain)], ['site' => []]);

        return Inertia::render('SiteDetail', $d + ['domain' => $domain]);
    }

    public function databases(DbManagerClient $db, CoreAgent $core): Response
    {
        $d = $this->load(fn () => ['engines' => $db->engines(), 'databases' => $db->databases()], ['engines' => [], 'databases' => []]);
        try {
            $all = $core->sites();
            $sites = array_column($all, 'domain');
            $tls = collect($all)->mapWithKeys(fn ($s) => [$s['domain'] => (string) ($s['tls'] ?? '') !== ''])->all();
            $pma = array_values(array_filter($core->apps(), fn ($a) => ($a['app'] ?? '') === 'phpmyadmin'));
        } catch (CoreAgentUnavailable) {
            $sites = $tls = $pma = [];
        }

        return Inertia::render('Databases', $d + ['sites' => $sites, 'tls' => (object) $tls, 'pma' => $pma]);
    }

    public function backups(BackupClient $b, CoreAgent $core, DbManagerClient $db): Response
    {
        $d = $this->load(fn () => ['destinations' => $b->destinations(), 'jobs' => $b->jobs(), 'runs' => $b->runs()],
            ['destinations' => [], 'jobs' => [], 'runs' => []]);
        try {
            $sites = collect($core->sites())->map(fn ($s) => ['domain' => $s['domain'], 'user' => $s['user']])->all();
        } catch (CoreAgentUnavailable) {
            $sites = [];
        }
        try {
            $databases = collect($db->databases())->map(fn ($x) => ['value' => $x['engine'].':'.$x['name'], 'label' => $x['engine'].' · '.$x['name']])->all();
        } catch (CoreAgentUnavailable) {
            $databases = [];
        }

        return Inertia::render('Backups', $d + ['sites' => $sites, 'databases' => $databases]);
    }

    public function mail(MailClient $m): Response
    {
        $d = $this->load(fn () => [
            'settings' => $m->settings(),
            'domains' => $m->domains(),
            'mailboxes' => collect($m->mailboxes())->groupBy('domain')->map->values()->all(),
        ], ['settings' => [], 'domains' => [], 'mailboxes' => (object) []]);

        return Inertia::render('Mail', $d);
    }

    public function php(CoreAgent $core): Response
    {
        $d = $this->load(function () use ($core) {
            $p = $core->php();

            return ['runtimes' => $p['runtimes'], 'extensions' => $p['extensions']];
        }, ['runtimes' => [], 'extensions' => []]);

        return Inertia::render('Php', $d);
    }

    public function tasks(CoreAgent $core, SystemClient $system): Response
    {
        $d = $this->load(fn () => ['tasks' => array_map(fn ($t) => $t + ['service' => 'core'], $core->tasks())], ['tasks' => []]);
        try {
            $d['tasks'] = array_merge($d['tasks'], array_map(fn ($t) => $t + ['service' => 'system'], $system->tasks()));
            $d['available'] = true;
        } catch (CoreAgentUnavailable) {
        }
        usort($d['tasks'], fn ($a, $b) => strcmp($b['created_at'] ?? '', $a['created_at'] ?? ''));

        return Inertia::render('Tasks', $d);
    }

    public function dashboard(SystemClient $system, CoreAgent $core, DbManagerClient $db, MailClient $mail): Response
    {
        $d = $this->load(fn () => ['stats' => $system->stats()], ['stats' => null]);
        $counts = ['sites' => null, 'databases' => null, 'domains' => null];
        foreach ([
            'sites' => fn () => count($core->sites()),
            'databases' => fn () => count($db->databases()),
            'domains' => fn () => count($mail->domains()),
        ] as $k => $fn) {
            try {
                $counts[$k] = $fn();
            } catch (CoreAgentUnavailable) {
            }
        }

        return Inertia::render('Dashboard', $d + ['counts' => $counts]);
    }

    public function software(SystemClient $system): Response
    {
        return Inertia::render('Software', $this->load(fn () => $system->software(), ['items' => [], 'categories' => []]));
    }

    public function security(Request $request, SystemClient $system): Response
    {
        return Inertia::render('Security', $this->load(fn () => [
            'firewall' => $system->firewall(), 'fail2ban' => $system->fail2ban(), 'ssh' => $system->ssh(), 'cadwaf' => $system->cadwaf(),
            'clientIp' => (string) $request->ip(),
        ], ['firewall' => null, 'fail2ban' => null, 'ssh' => null, 'cadwaf' => null, 'clientIp' => '']));
    }

    public function docker(SystemClient $system): Response
    {
        return Inertia::render('Docker', $this->load(fn () => ['docker' => $system->docker()], ['docker' => null]));
    }

    public function ftp(SystemClient $system, CoreAgent $core): Response
    {
        $d = $this->load(fn () => ['ftp' => $system->ftp()], ['ftp' => null]);
        try {
            $sites = collect($core->sites())->map(fn ($s) => ['domain' => $s['domain'], 'user' => $s['user']])->all();
        } catch (CoreAgentUnavailable) {
            $sites = [];
        }

        return Inertia::render('Ftp', $d + ['sites' => $sites]);
    }

    public function scheduler(SystemClient $system): Response
    {
        return Inertia::render('Scheduler', $this->load(fn () => ['jobs' => $system->cron()], ['jobs' => []]));
    }

    public function logs(SystemClient $system): Response
    {
        return Inertia::render('Logs', $this->load(fn () => [
            'sources' => $system->logs(),
            'units' => array_column($system->services(), 'unit'),
        ], ['sources' => [], 'units' => []]));
    }

    public function updates(SystemClient $system): Response
    {
        return Inertia::render('Updates', $this->load(fn () => ['updates' => $system->updates()], ['updates' => null]));
    }

    public function services(CoreAgent $core, NotifierClient $notifier, DbManagerClient $db, BackupClient $backup, MailClient $mail, SystemClient $system, \Illuminate\Http\Request $request): Response
    {
        $all = $request->boolean('all');
        try {
            $units = $system->services($all);
        } catch (CoreAgentUnavailable) {
            $units = [];
        }
        try {
            $checks = $notifier->status();
        } catch (CoreAgentUnavailable) {
            $checks = [];
        }
        $services = [
            ['name' => 'Panel', 'up' => true, 'note' => __('You are looking at it')],
            ['name' => 'Core', 'up' => $core->isHealthy(), 'note' => __('Sites, PHP, web servers')],
            ['name' => 'Databases', 'up' => $db->isHealthy(), 'note' => __('Database servers and databases')],
            ['name' => 'Backups', 'up' => $backup->isHealthy(), 'note' => __('Scheduled backups and restores')],
            ['name' => 'Mail', 'up' => $mail->isHealthy(), 'note' => __('Mail domains and mailboxes')],
            ['name' => 'System', 'up' => $system->isHealthy(), 'note' => __('Monitoring, software, firewall, Docker, FTP')],
            ['name' => 'Notifier', 'up' => $notifier->isHealthy(), 'note' => __('Telegram and email alerts')],
        ];

        return Inertia::render('Services', compact('services', 'checks', 'units', 'all'));
    }
}
