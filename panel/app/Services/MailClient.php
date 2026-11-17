<?php

namespace App\Services;

/** Client for the mail Service. */
class MailClient extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.mail_socket');
    }

    public function settings(): array
    {
        return $this->call('GET', '/v1/settings')['data'] ?? [];
    }

    public function saveSettings(array $s): array
    {
        return $this->call('PUT', '/v1/settings', $s);
    }

    public function install(): array
    {
        return $this->call('POST', '/v1/install');
    }

    public function domains(): array
    {
        return $this->call('GET', '/v1/domains')['data'] ?? [];
    }

    public function addDomain(string $name): array
    {
        return $this->call('POST', '/v1/domains', ['name' => $name]);
    }

    public function deleteDomain(string $name): array
    {
        return $this->call('DELETE', '/v1/domains/'.rawurlencode($name));
    }

    public function dns(string $name): array
    {
        return $this->call('GET', '/v1/domains/'.rawurlencode($name).'/dns');
    }

    public function mailboxes(string $domain = ''): array
    {
        return $this->call('GET', '/v1/mailboxes?domain='.rawurlencode($domain))['data'] ?? [];
    }

    public function addMailbox(string $email, string $password): array
    {
        return $this->call('POST', '/v1/mailboxes', ['email' => $email, 'password' => $password]);
    }

    public function setPassword(int $id, string $password): array
    {
        return $this->call('PUT', "/v1/mailboxes/$id/password", ['password' => $password]);
    }

    public function deleteMailbox(int $id): array
    {
        return $this->call('DELETE', "/v1/mailboxes/$id");
    }

    public function webmail(int $id): array
    {
        return $this->call('POST', "/v1/mailboxes/$id/webmail");
    }

    public function task(string $id): ?array
    {
        return $this->call('GET', '/v1/tasks/'.rawurlencode($id))['data'];
    }
}
