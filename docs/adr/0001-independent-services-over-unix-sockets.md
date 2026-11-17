# Panel split into independent Services over Unix sockets

The Panel (Laravel) holds no system privileges and no site configuration. Separate Go Services (core agent, backup, mail, database, notifier), each its own systemd unit with its own sqlite state, talk to the Panel and each other over Unix sockets using JSON over HTTP (chosen over gRPC because PHP has no practical gRPC client; a Service can still add gRPC later). Each Service owns its data (Core Agent: sites, users, pools; mail: domains and mailboxes; backup: jobs), so a failing or lost Panel or Service never takes down sites or other Services, and the Core Agent can rebuild all web configuration without the Panel. Rejected: a Laravel monolith, because a backup or mail fault would take down the whole panel.

## Consequences

Long operations are asynchronous Tasks with progress streamed to the UI. Single server only; no cluster.
