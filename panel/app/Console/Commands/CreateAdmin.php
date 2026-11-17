<?php

namespace App\Console\Commands;

use App\Models\User;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Hash;
use PragmaRX\Google2FA\Google2FA;

class CreateAdmin extends Command
{
    protected $signature = 'panel:admin {email} {--password= : Omit to be prompted}';

    protected $description = 'Create the single Administrator account and print its TOTP secret';

    public function handle(Google2FA $g): int
    {
        if (User::query()->exists()) {
            $this->error('An Administrator already exists. The panel supports exactly one.');

            return self::FAILURE;
        }
        $password = $this->option('password') ?: $this->secret('Password (min 12 chars)');
        if (strlen((string) $password) < 12) {
            $this->error('Password must be at least 12 characters.');

            return self::FAILURE;
        }
        $secret = $g->generateSecretKey();
        User::create([
            'name' => 'Administrator',
            'email' => $this->argument('email'),
            'password' => Hash::make($password),
            'totp_secret' => $secret,
        ]);
        $uri = $g->getQRCodeUrl('Hosting Panel', $this->argument('email'), $secret);
        $this->info('Administrator created. Add this to your authenticator app:');
        $this->line("Secret: $secret");
        $this->line("URI:    $uri");

        return self::SUCCESS;
    }
}
