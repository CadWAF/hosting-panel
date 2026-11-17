# nginx is the only public entry point; Apache is an optional local backend

nginx always owns ports 80/443 (TLS, static files, ACME, proxying). A Site's Web Mode is either nginx serving PHP-FPM directly, or nginx proxying to a local Apache bound to 127.0.0.1 for `.htaccess` and mod_* needs. Switching a Site's mode never changes its public setup. Rejected: choosing nginx or Apache as the public server per Site, which is impossible on shared ports and 80/443.

## Consequences

Apache never listens publicly. Any future WAF attaches in front of or inside nginx.
