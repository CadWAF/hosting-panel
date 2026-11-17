# Per-Site Linux user and PHP-FPM Pool, native packages instead of Docker

Each Site runs as its own Linux user with its own PHP-FPM Pool on a chosen PHP Runtime (parallel versions from the Ondřej Surý apt repository), under per-pool cgroup limits. Database Servers are native packages, one version per engine. Chosen over containers to avoid a container runtime, keep backups simple and match what a PHP-only host needs. Docker is added only when a workload needs it (e.g. Node/Python apps, third-party service images).

## Consequences

A PHP Runtime cannot be removed while any Site uses it. Isolation relies on Unix permissions, open_basedir and cgroups, not on kernel-level container isolation.
