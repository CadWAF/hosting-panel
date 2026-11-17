<?php

namespace App\Http\Middleware;

use Illuminate\Http\Request;
use Inertia\Middleware;

class HandleInertiaRequests extends Middleware
{
    protected $rootView = 'app';

    private function locale(Request $r): string
    {
        $c = $r->cookie('hp_locale');
        if ($c === 'ru' || $c === 'en') {
            return $c;
        }

        return str_starts_with((string) $r->getPreferredLanguage(['en', 'ru']), 'ru') ? 'ru' : 'en';
    }

    public function share(Request $request): array
    {
        return [
            ...parent::share($request),
            'auth' => ['email' => $request->user()?->email],
            'defaultPhp' => config('panel.default_php'),
            'locale' => $this->locale($request),
        ];
    }
}
