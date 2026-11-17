<?php

namespace App\Services;

/** Client for the backup Service. */
class BackupClient extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.backup_socket');
    }

    public function destinations(): array
    {
        return $this->call('GET', '/v1/destinations')['data'] ?? [];
    }

    public function addDestination(array $in): array
    {
        return $this->call('POST', '/v1/destinations', $in);
    }

    public function testDestination(int $id): array
    {
        return $this->call('POST', "/v1/destinations/$id/test");
    }

    public function deleteDestination(int $id): array
    {
        return $this->call('DELETE', "/v1/destinations/$id");
    }

    public function jobs(): array
    {
        return $this->call('GET', '/v1/jobs')['data'] ?? [];
    }

    public function addJob(array $in): array
    {
        return $this->call('POST', '/v1/jobs', $in);
    }

    public function deleteJob(int $id): array
    {
        return $this->call('DELETE', "/v1/jobs/$id");
    }

    public function runJob(int $id): array
    {
        return $this->call('POST', "/v1/jobs/$id/run");
    }

    public function runs(int $job = 0): array
    {
        return $this->call('GET', "/v1/runs?job=$job")['data'] ?? [];
    }

    public function restore(int $run, array $in): array
    {
        return $this->call('POST', "/v1/runs/$run/restore", $in);
    }

    public function installBorg(): array
    {
        return $this->call('POST', '/v1/borg/install');
    }

    public function task(string $id): ?array
    {
        return $this->call('GET', '/v1/tasks/'.rawurlencode($id))['data'];
    }
}
