<?php

namespace App\Services;

/** Client for the Notifier. */
class NotifierClient extends ServiceClient
{
    protected function socket(): string
    {
        return config('panel.notifier_socket');
    }

    public function status(): array
    {
        return $this->call('GET', '/v1/status')['data'] ?? [];
    }
}
