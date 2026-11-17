package sites

import (
	"bytes"
	"sort"
	"text/template"
)

type tmplData struct {
	Domain, User, PHPVersion, Home, Docroot, Socket, LogDir string
	ApachePort                                              int
	MaxChildren                                             int
	Terminate                                               string
	Settings                                                []kv
	TLS                                                     bool
	SSLNames                                                string
	MaxBody, Pre                                            string
	RootProxy                                               bool
}

type kv struct{ K, V string }

func sortedKV(m map[string]string) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

var poolTmpl = template.Must(template.New("pool").Parse(`; managed by hostingpanel core-agent - do not edit
[{{.User}}]
user = {{.User}}
group = {{.User}}
listen = {{.Socket}}
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = {{.MaxChildren}}
pm.process_idle_timeout = 30s
request_terminate_timeout = {{.Terminate}}
php_admin_value[open_basedir] = {{.Home}}:/tmp
php_admin_value[upload_tmp_dir] = {{.Home}}/tmp
php_admin_value[session.save_path] = {{.Home}}/tmp
php_admin_value[error_log] = {{.LogDir}}/php-error.log
php_admin_flag[log_errors] = on
{{range .Settings}}php_admin_value[{{.K}}] = {{.V}}
{{end}}`))

const acmeLocation = `    location ^~ /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
        default_type text/plain;
        auth_basic off;
    }
`

const tlsDirectives = `    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    add_header Strict-Transport-Security "max-age=31536000" always;
`

// vhost wraps a per-mode body into the port-80 server (certificate challenge + redirect when TLS is on)
// and, when TLS is on, a port-443 server with the same body.
func vhost(banner, body string) *template.Template {
	t := `# managed by hostingpanel core-agent - do not edit` + banner + `
{{define "body"}}` + body + `{{end}}
server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}} www.{{.Domain}};
    access_log {{.LogDir}}/access.log;
` + acmeLocation + `{{if .TLS}}    location / {
        return 301 https://$host$request_uri;
    }
}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.SSLNames}};
` + tlsDirectives + `{{template "body" .}}
}
{{else}}{{template "body" .}}
}
{{end}}`
	return template.Must(template.New("vhost").Parse(t))
}

var nginxPHPTmpl = vhost("", `    root {{.Docroot}};
    index index.php index.html;
    error_log {{.LogDir}}/error.log;
    client_max_body_size {{.MaxBody}};
{{.Pre}}{{if not .RootProxy}}
    location / {
        try_files $uri $uri/ /index.php?$args;
    }
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:{{.Socket}};
    }
    location ~ /\.(?!well-known) {
        deny all;
    }
{{end}}`)

var nginxProxyTmpl = vhost(" (Apache backend)", `    error_log {{.LogDir}}/error.log;
    client_max_body_size {{.MaxBody}};
{{.Pre}}{{if not .RootProxy}}
    location / {
        proxy_pass http://127.0.0.1:{{.ApachePort}};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
{{end}}`)

var apacheTmpl = template.Must(template.New("apache").Parse(`# managed by hostingpanel core-agent - do not edit
Listen 127.0.0.1:{{.ApachePort}}
<VirtualHost 127.0.0.1:{{.ApachePort}}>
    ServerName {{.Domain}}
    DocumentRoot {{.Docroot}}
    ErrorLog {{.LogDir}}/apache-error.log
    <Directory {{.Docroot}}>
        AllowOverride All
        Options -Indexes +FollowSymLinks
        Require all granted
        DirectoryIndex index.php index.html
    </Directory>
    <FilesMatch "\.php$">
        SetHandler "proxy:unix:{{.Socket}}|fcgi://localhost"
    </FilesMatch>
</VirtualHost>
`))

var disabledTmpl = vhost(" (site disabled)", `    location / {
        default_type text/html;
        return 503 '<!doctype html><meta charset="utf-8"><title>Site disabled</title><body style="font:16px system-ui;display:grid;place-items:center;height:100vh;margin:0"><p>This site is temporarily unavailable.</p>';
    }
`)

func render(t *template.Template, d tmplData) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}
