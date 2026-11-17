<?php

namespace App\Http\Controllers;

use App\Services\BackupClient;
use App\Services\CoreAgent;
use App\Services\CoreAgentUnavailable;
use App\Services\DbManagerClient;
use App\Services\MailClient;
use App\Services\SystemClient;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\StreamedResponse;

/**
 * One authenticated endpoint through which the UI calls a fixed set of Service operations.
 * Only methods listed here are reachable; the Services validate their own input.
 */
class RpcController extends Controller
{
    private const ALLOWED = [
        'core' => [CoreAgent::class, ['createSite', 'updateSite', 'setSiteEnabled', 'deleteSite', 'installPhp', 'removePhp', 'installExtension',
            'removeExtension', 'installApache', 'site', 'issueTls', 'removeTls', 'task', 'sshKeys', 'addSshKey', 'generateSshKey', 'removeSshKey',
            'cron', 'addCron', 'removeCron', 'log', 'files', 'readFile', 'writeFile', 'fileOp']],
        'db' => [DbManagerClient::class, ['installEngine', 'createDatabase', 'resetPassword', 'deleteDatabase', 'task']],
        'backup' => [BackupClient::class, ['addDestination', 'testDestination', 'deleteDestination', 'addJob', 'deleteJob', 'runJob', 'restore', 'installBorg', 'task']],
        'system' => [SystemClient::class, ['stats', 'services', 'installSoftware', 'removeSoftware', 'serviceAction', 'setJail', 'setF2bDefaults', 'ban', 'unban',
            'enableFirewall', 'disableFirewall', 'addRule', 'deleteRule', 'setSsh', 'checkUpdates', 'applyUpdates', 'reboot', 'addCron', 'updateCron',
            'deleteCron', 'runCron', 'readLog', 'runContainer', 'containerAction', 'containerLog', 'pullImage', 'removeImage', 'addFtp',
            'setFtpPassword', 'deleteFtp', 'openFtpPorts', 'task']],
        'mail' => [MailClient::class, ['saveSettings', 'install', 'addDomain', 'deleteDomain', 'dns', 'addMailbox', 'setPassword', 'deleteMailbox', 'webmail', 'task']],
    ];

    public function call(Request $r): JsonResponse
    {
        // Raw body on purpose: the global middleware would turn '' into null and trim file contents.
        $body = json_decode($r->getContent(), true);
        $body = is_array($body) ? $body : [];
        $service = (string) ($body['service'] ?? '');
        $method = (string) ($body['method'] ?? '');
        $args = array_values((array) ($body['args'] ?? []));
        [$class, $methods] = self::ALLOWED[$service] ?? [null, []];
        if (! $class || ! in_array($method, $methods, true) || count($args) > 6) {
            return response()->json(['ok' => false, 'error' => 'Unknown operation'], 422);
        }
        try {
            $out = app($class)->{$method}(...$args);
        } catch (CoreAgentUnavailable) {
            return response()->json(['ok' => false, 'unavailable' => true, 'error' => __('The service is not running.')]);
        } catch (\TypeError|\ArgumentCountError) {
            return response()->json(['ok' => false, 'error' => __('Invalid input.')], 422);
        }
        if (! is_array($out) || ! array_key_exists('ok', $out)) {
            $out = ['ok' => true, 'data' => $out, 'error' => null, 'extra' => []];
        }

        return response()->json($out);
    }

    public function upload(Request $r, string $domain, CoreAgent $core): JsonResponse
    {
        $r->validate(['file' => ['required', 'file', 'max:262144'], 'path' => ['nullable', 'string']]);
        $file = $r->file('file');
        $dest = ltrim(trim((string) $r->input('path'), '/').'/'.basename($file->getClientOriginalName()), '/');
        $stream = fopen($file->getRealPath(), 'rb');
        try {
            $out = $core->uploadFile($domain, $dest, $stream);
        } catch (CoreAgentUnavailable) {
            return response()->json(['ok' => false, 'error' => __('The service is not running.')]);
        } finally {
            is_resource($stream) && fclose($stream);
        }

        return response()->json(['ok' => $out['ok'], 'error' => $out['error']]);
    }

    public function download(Request $r, string $domain, CoreAgent $core): StreamedResponse
    {
        $path = (string) $r->query('path');
        $up = $core->download($domain, $path);
        abort_unless($up->getStatusCode() === 200, 404);

        return response()->stream(function () use ($up) {
            $b = $up->getBody();
            while (! $b->eof()) {
                echo $b->read(65536);
                flush();
            }
        }, 200, ['Content-Type' => 'application/octet-stream',
            'Content-Disposition' => 'attachment; filename="'.addcslashes(basename($path), '"\\').'"']);
    }
}
