<?php

use App\Http\Controllers\AuthController;
use App\Http\Controllers\PageController;
use App\Http\Controllers\RpcController;
use Illuminate\Support\Facades\Route;


Route::post('/locale', [AuthController::class, 'locale'])->name('locale');

Route::middleware('guest')->group(function () {
    Route::get('/login', [AuthController::class, 'show'])->name('login');
    Route::post('/login/password', [AuthController::class, 'password']);
    Route::post('/login/code', [AuthController::class, 'code']);
});
Route::post('/logout', [AuthController::class, 'logout'])->name('logout')->middleware('auth');

Route::middleware('auth')->group(function () {
    Route::get('/', [PageController::class, 'dashboard'])->name('dashboard');
    Route::get('/sites', [PageController::class, 'sites'])->name('sites');
    Route::get('/sites/{domain}', [PageController::class, 'site'])->name('site')->where('domain', '[A-Za-z0-9.\-]+');
    Route::get('/databases', [PageController::class, 'databases'])->name('databases');
    Route::get('/backups', [PageController::class, 'backups'])->name('backups');
    Route::get('/mail', [PageController::class, 'mail'])->name('mail');
    Route::get('/software', [PageController::class, 'software'])->name('software');
    Route::get('/security', [PageController::class, 'security'])->name('security');
    Route::get('/docker', [PageController::class, 'docker'])->name('docker');
    Route::get('/ftp', [PageController::class, 'ftp'])->name('ftp');
    Route::get('/scheduler', [PageController::class, 'scheduler'])->name('scheduler');
    Route::get('/logs', [PageController::class, 'logs'])->name('logs');
    Route::get('/updates', [PageController::class, 'updates'])->name('updates');
    Route::get('/php', [PageController::class, 'php'])->name('php');
    Route::get('/tasks', [PageController::class, 'tasks'])->name('tasks');
    Route::get('/services', [PageController::class, 'services'])->name('services');

    Route::post('/rpc', [RpcController::class, 'call'])->name('rpc');
    Route::post('/sites/{domain}/upload', [RpcController::class, 'upload'])->name('site.upload')->where('domain', '[A-Za-z0-9.\-]+');
    Route::get('/sites/{domain}/download', [RpcController::class, 'download'])->name('site.download')->where('domain', '[A-Za-z0-9.\-]+');
});
