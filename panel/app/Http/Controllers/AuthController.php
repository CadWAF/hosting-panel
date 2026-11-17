<?php

namespace App\Http\Controllers;

use App\Models\User;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\RateLimiter;
use Inertia\Inertia;
use Inertia\Response;
use PragmaRX\Google2FA\Google2FA;

class AuthController extends Controller
{
    private function key(Request $r): string
    {
        return 'login|'.$r->ip();
    }

    private function blocked(Request $r): ?JsonResponse
    {
        if (RateLimiter::tooManyAttempts($this->key($r), 5)) {
            return response()->json(['error' => __('Too many attempts. Try again in :s seconds.', ['s' => RateLimiter::availableIn($this->key($r))])], 429);
        }

        return null;
    }

    public function show(): Response
    {
        return Inertia::render('Login');
    }

    public function password(Request $r): JsonResponse
    {
        if ($b = $this->blocked($r)) {
            return $b;
        }
        $data = $r->validate(['email' => ['required', 'string'], 'password' => ['required', 'string']]);
        $user = User::where('email', $data['email'])->first();
        if (! $user || ! Hash::check($data['password'], $user->password)) {
            RateLimiter::hit($this->key($r), 300);

            return response()->json(['error' => __('Wrong email or password.')], 422);
        }
        session(['login.pending' => $user->id, 'login.pending_at' => now()->timestamp]);

        return response()->json(['step' => 'code']);
    }

    public function code(Request $r, Google2FA $g): JsonResponse
    {
        if ($b = $this->blocked($r)) {
            return $b;
        }
        $id = session('login.pending');
        if (! $id || now()->timestamp - (int) session('login.pending_at') > 300) {
            return response()->json(['error' => __('Session expired, sign in again.'), 'step' => 'password'], 422);
        }
        $user = User::find($id);
        $code = preg_replace('/\D/', '', (string) $r->input('code'));
        if (! $user || ! $g->verifyKey($user->totp_secret, $code, 1)) {
            RateLimiter::hit($this->key($r), 300);

            return response()->json(['error' => __('Wrong code.')], 422);
        }
        RateLimiter::clear($this->key($r));
        session()->forget(['login.pending', 'login.pending_at']);
        Auth::login($user);
        $r->session()->regenerate();

        return response()->json(['redirect' => route('sites')]);
    }

    public function logout(Request $r)
    {
        Auth::logout();
        $r->session()->invalidate();
        $r->session()->regenerateToken();

        return Inertia::location(route('login'));
    }
}
