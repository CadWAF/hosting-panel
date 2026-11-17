<?php

namespace App\Services;

use Illuminate\Http\Client\ConnectionException;
use Illuminate\Http\Client\PendingRequest;
use Illuminate\Support\Facades\Http;

/** Base client for a Service reached as JSON over HTTP on a Unix socket. */
abstract class ServiceClient
{
    abstract protected function socket(): string;

    private function http(): PendingRequest
    {
        return Http::baseUrl('http://service')
            ->withOptions(['curl' => [CURLOPT_UNIX_SOCKET_PATH => $this->socket()]])
            ->timeout(30)->acceptJson()->asJson();
    }

    /** @return array{ok:bool,status:int,data:mixed,error:?string,extra:array} */
    protected function call(string $method, string $path, array $body = []): array
    {
        try {
            $r = $this->http()->send($method, $path, $body ? ['json' => $body] : []);
        } catch (ConnectionException $e) {
            throw new CoreAgentUnavailable('Service is not reachable', 0, $e);
        }

        return [
            'ok' => $r->successful(),
            'status' => $r->status(),
            'data' => $r->json('data'),
            'error' => $r->json('error'),
            'extra' => (array) $r->json(),
        ];
    }

    /** Stream a raw response body (downloads) without loading it into memory. */
    public function stream(string $method, string $path, array $body = []): \Psr\Http\Message\ResponseInterface
    {
        try {
            $r = $this->http()->withOptions(['stream' => true, 'curl' => [CURLOPT_UNIX_SOCKET_PATH => $this->socket()]])
                ->timeout(0)->send($method, $path, $body ? ['json' => $body] : []);
        } catch (ConnectionException $e) {
            throw new CoreAgentUnavailable('Service is not reachable', 0, $e);
        }

        return $r->toPsrResponse();
    }

    /** Send a raw body (uploads, file saves) from a string or stream resource. */
    protected function putRaw(string $path, $body): array
    {
        try {
            $r = $this->http()->timeout(0)->withBody($body, 'application/octet-stream')->put($path);
        } catch (ConnectionException $e) {
            throw new CoreAgentUnavailable('Service is not reachable', 0, $e);
        }

        return ['ok' => $r->successful(), 'status' => $r->status(), 'data' => $r->json('data'), 'error' => $r->json('error'), 'extra' => (array) $r->json()];
    }

    public function isHealthy(): bool
    {
        try {
            return $this->call('GET', '/v1/health')['ok'];
        } catch (CoreAgentUnavailable) {
            return false;
        }
    }
}
