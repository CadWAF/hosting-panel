<?php

namespace App\Services;

/** Client for the database manager Service. */
class DbManagerClient extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.db_socket');
    }

    public function engines(): array
    {
        return $this->call('GET', '/v1/engines')['data'] ?? [];
    }

    public function installEngine(string $engine, string $version): array
    {
        return $this->call('POST', "/v1/engines/$engine/install", ['version' => $version]);
    }

    public function databases(): array
    {
        return $this->call('GET', '/v1/databases')['data'] ?? [];
    }

    public function createDatabase(string $engine, string $name, string $site): array
    {
        return $this->call('POST', '/v1/databases', ['engine' => $engine, 'name' => $name, 'site' => $site]);
    }

    public function resetPassword(int $id): array
    {
        return $this->call('POST', "/v1/databases/$id/password");
    }

    public function deleteDatabase(int $id): array
    {
        return $this->call('DELETE', "/v1/databases/$id");
    }

    public function task(string $id): ?array
    {
        return $this->call('GET', '/v1/tasks/'.rawurlencode($id))['data'];
    }
}
