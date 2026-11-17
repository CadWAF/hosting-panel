<?php

namespace Tests\Feature;

use App\Models\User;
use App\Services\CoreAgent;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Inertia\Testing\AssertableInertia as Assert;
use PragmaRX\Google2FA\Google2FA;
use Tests\TestCase;

class PanelTest extends TestCase
{
    use RefreshDatabase;

    private function admin(): User
    {
        return User::create([
            'name' => 'Administrator', 'email' => 'a@example.com',
            'password' => bcrypt('correct horse battery'), 'totp_secret' => (new Google2FA)->generateSecretKey(),
        ]);
    }

    private function rpc(string $service, string $method, array $args = [])
    {
        return $this->postJson('/rpc', ['service' => $service, 'method' => $method, 'args' => $args]);
    }

    public function test_guests_are_redirected_to_login(): void
    {
        $this->get('/sites')->assertRedirect('/login');
        $this->postJson('/rpc', ['service' => 'core', 'method' => 'sites'])->assertUnauthorized();
    }

    public function test_login_page_renders(): void
    {
        $this->get('/login')->assertOk()->assertInertia(fn (Assert $p) => $p->component('Login'));
    }

    public function test_login_requires_password_then_valid_totp(): void
    {
        $u = $this->admin();
        $this->postJson('/login/password', ['email' => 'a@example.com', 'password' => 'wrong'])->assertStatus(422)->assertJson(['error' => 'Wrong email or password.']);
        $this->postJson('/login/password', ['email' => 'a@example.com', 'password' => 'correct horse battery'])->assertOk()->assertJson(['step' => 'code']);
        $this->assertGuest();
        $this->postJson('/login/code', ['code' => '000000'])->assertStatus(422)->assertJson(['error' => 'Wrong code.']);
        $this->assertGuest();
        $this->postJson('/login/code', ['code' => (new Google2FA)->getCurrentOtp($u->totp_secret)])->assertOk()->assertJsonStructure(['redirect']);
        $this->assertAuthenticatedAs($u);
    }

    public function test_code_step_cannot_be_reached_without_the_password_step(): void
    {
        $u = $this->admin();
        $this->postJson('/login/code', ['code' => (new Google2FA)->getCurrentOtp($u->totp_secret)])->assertStatus(422);
        $this->assertGuest();
    }

    public function test_login_is_rate_limited(): void
    {
        $this->admin();
        for ($i = 0; $i < 5; $i++) {
            $this->postJson('/login/password', ['email' => 'a@example.com', 'password' => 'bad'])->assertStatus(422);
        }
        $this->postJson('/login/password', ['email' => 'a@example.com', 'password' => 'correct horse battery'])
            ->assertStatus(429)->assertJsonPath('error', fn ($e) => str_contains($e, 'Too many attempts'));
    }

    public function test_sites_page_passes_sites_and_installed_php(): void
    {
        Http::fake(fn ($r) => match (true) {
            str_contains($r->url(), '/v1/php') => Http::response(['data' => [['version' => '8.3', 'installed' => true, 'sites' => [], 'extensions' => []]], 'extensions' => ['gd']]),
            str_contains($r->url(), '/v1/setting-keys') => Http::response(['data' => ['memory_limit']]),
            default => Http::response(['data' => [['domain' => 'x.com', 'user' => 'u1', 'php_version' => '8.3', 'status' => 'enabled']]]),
        });
        $this->actingAs($this->admin());
        $this->get('/sites')->assertOk()->assertInertia(fn (Assert $p) => $p
            ->component('Sites')->where('available', true)->where('installed', ['8.3'])->has('sites', 1)->where('sites.0.domain', 'x.com'));
    }

    public function test_pages_survive_unreachable_services(): void
    {
        $this->actingAs($this->admin());
        config(['panel.core_socket' => '/nonexistent/core.sock', 'panel.db_socket' => '/nonexistent/db.sock',
            'panel.backup_socket' => '/nonexistent/b.sock', 'panel.mail_socket' => '/nonexistent/m.sock']);
        $this->assertFalse(app(CoreAgent::class)->isHealthy());
        foreach (['sites' => 'Sites', 'databases' => 'Databases', 'backups' => 'Backups', 'mail' => 'Mail', 'php' => 'Php', 'tasks' => 'Tasks', 'sites/x.com' => 'SiteDetail'] as $url => $component) {
            $this->get("/$url")->assertOk()->assertInertia(fn (Assert $p) => $p->component($component)->where('available', false));
        }
    }

    public function test_services_page_lists_each_service_independently(): void
    {
        Http::fake(['*' => Http::response(['data' => []])]);
        $this->actingAs($this->admin());
        config(['panel.mail_socket' => '/nonexistent/m.sock']);
        $this->get('/services')->assertOk()->assertInertia(fn (Assert $p) => $p->component('Services')->has('services', 6));
    }

    public function test_rpc_only_allows_listed_operations(): void
    {
        Http::fake();
        $this->actingAs($this->admin());
        $this->rpc('core', 'isHealthy')->assertStatus(422);
        $this->rpc('core', 'socket')->assertStatus(422);
        $this->rpc('shell', 'exec', ['id'])->assertStatus(422);
        Http::assertNothingSent();
    }

    public function test_rpc_forwards_a_site_creation_and_returns_the_agent_result(): void
    {
        Http::fake(['*' => Http::response(['data' => ['domain' => 'x.com']], 201)]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'createSite', ['x.com', '8.3', 'nginx', 'medium'])->assertOk()->assertJson(['ok' => true, 'data' => ['domain' => 'x.com']]);
        Http::assertSent(fn ($r) => $r->method() === 'POST' && $r['domain'] === 'x.com' && $r['php_version'] === '8.3' && $r['preset'] === 'medium');
    }

    public function test_agent_error_is_returned_in_plain_language(): void
    {
        Http::fake(['*' => Http::response(['error' => 'That domain already exists.'], 409)]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'createSite', ['x.com', '8.3'])->assertOk()->assertJson(['ok' => false, 'error' => 'That domain already exists.']);
    }

    public function test_rpc_reports_a_dead_service_without_crashing(): void
    {
        $this->actingAs($this->admin());
        config(['panel.core_socket' => '/nonexistent/core.sock']);
        $this->rpc('core', 'deleteSite', ['x.com'])->assertOk()->assertJson(['ok' => false, 'unavailable' => true]);
    }

    public function test_site_settings_send_php_ini_overrides_as_an_object(): void
    {
        Http::fake(['*' => Http::response(['data' => []])]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'updateSite', ['x.com', ['php_version' => '8.3', 'php_settings' => []]])->assertOk();
        Http::assertSent(fn ($r) => $r->method() === 'PATCH' && str_contains($r->body(), '"php_settings":{}'));
    }

    public function test_database_password_comes_back_in_extra_only_once(): void
    {
        Http::fake(['*' => Http::response(['data' => ['id' => 1, 'name' => 'shop', 'user' => 'shop'], 'password' => 'p4ss'], 201)]);
        $this->actingAs($this->admin());
        $this->rpc('db', 'createDatabase', ['MariaDB', 'shop', ''])->assertOk()->assertJsonPath('extra.password', 'p4ss')->assertJsonPath('data.name', 'shop');
    }

    public function test_generated_ssh_private_key_is_returned_to_the_caller(): void
    {
        Http::fake(['*' => Http::response(['data' => ['id' => 2, 'label' => 'generated'], 'private_key' => 'PRIVATE'], 201)]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'generateSshKey', ['x.com', 'generated'])->assertOk()->assertJsonPath('extra.private_key', 'PRIVATE');
    }

    public function test_file_listing_and_file_ops_go_through_the_agent(): void
    {
        Http::fake(fn ($r) => str_contains($r->url(), '/files')
            ? Http::response(['data' => [['name' => 'public_html', 'dir' => true, 'size' => 0, 'mode' => '0750']]])
            : Http::response(['data' => []]));
        $this->actingAs($this->admin());
        $this->rpc('core', 'files', ['x.com', ''])->assertOk()->assertJsonPath('data.0.name', 'public_html');
        $this->rpc('core', 'fileOp', ['x.com', 'trash', ['path' => 'old.txt']])->assertOk();
        Http::assertSent(fn ($r) => $r->method() === 'POST' && str_contains($r->body(), 'old.txt'));
    }

    public function test_agent_rejection_of_a_bad_path_is_returned(): void
    {
        Http::fake(['*' => Http::response(['error' => 'Path escapes the site folder.'], 400)]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'readFile', ['x.com', '../../etc/passwd'])->assertOk()->assertJson(['ok' => false, 'error' => 'Path escapes the site folder.']);
    }

    public function test_download_route_requires_login(): void
    {
        $this->get('/sites/x.com/download?path=a.txt')->assertRedirect('/login');
    }

    public function test_backup_job_is_sent_exactly_as_the_page_built_it(): void
    {
        Http::fake(['*' => Http::response(['data' => ['id' => 5]], 201)]);
        $this->actingAs($this->admin());
        $job = ['name' => 'nightly', 'site' => 'x.com', 'home_path' => '/srv/sites/u1', 'databases' => [['engine' => 'MariaDB', 'name' => 'shop']], 'destination_id' => 1, 'schedule' => '0 3 * * *', 'keep' => 7];
        $this->rpc('backup', 'addJob', [$job])->assertOk();
        Http::assertSent(fn ($r) => $r['home_path'] === '/srv/sites/u1' && $r['databases'][0]['name'] === 'shop');
    }

    public function test_restore_passes_the_chosen_mode(): void
    {
        Http::fake(['*' => Http::response(['data' => ['id' => 't1']], 202)]);
        $this->actingAs($this->admin());
        $this->rpc('backup', 'restore', [3, ['mode' => 'beside', 'files' => true, 'databases' => false]])->assertOk();
        Http::assertSent(fn ($r) => $r['mode'] === 'beside' && $r['databases'] === false);
    }

    public function test_mail_dns_checklist_and_webmail_url(): void
    {
        Http::fake(fn ($r) => match (true) {
            str_contains($r->url(), '/dns') => Http::response(['data' => [['purpose' => 'MX', 'type' => 'MX', 'name' => 'x.com', 'value' => 'mail.x.com', 'status' => 'ok', 'where' => 'dns']]]),
            str_contains($r->url(), '/webmail') => Http::response(['data' => ['url' => 'https://webmail.x.com/?_hp_sso=abc']]),
            default => Http::response(['data' => []]),
        });
        $this->actingAs($this->admin());
        $this->rpc('mail', 'dns', ['x.com'])->assertOk()->assertJsonPath('data.0.status', 'ok');
        $this->rpc('mail', 'webmail', [4])->assertOk()->assertJsonPath('data.url', 'https://webmail.x.com/?_hp_sso=abc');
    }

    public function test_https_request_carries_www_and_email(): void
    {
        Http::fake(['*' => Http::response(['data' => ['id' => 'tls1']], 202)]);
        $this->actingAs($this->admin());
        $this->rpc('core', 'issueTls', ['x.com', true, 'me@x.com'])->assertOk()->assertJsonPath('data.id', 'tls1');
        Http::assertSent(fn ($r) => $r['www'] === true && $r['email'] === 'me@x.com');
    }

    public function test_logout_ends_the_session(): void
    {
        $this->actingAs($this->admin());
        $this->post('/logout')->assertRedirect('/login');
        $this->assertGuest();
    }
}
