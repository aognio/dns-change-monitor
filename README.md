# dnsmon - DNS change monitor with Telegram notifications

`dnsmon` watches DNS records, detects the moment their answers change and
reports the change to a list of Telegram accounts as soon as it is live.

It is a Go daemon built as a set of small libraries that the command line
application composes into the running monitor. The design follows three
principles:

- **Domain driven.** The rules about records, changes, subscribers and
  pairing tokens live in packages that know nothing about HTTP, SQLite or
  Telegram.
- **Agent first.** The running behaviour is expressed as autonomous agents,
  each with a single goal, supervised by a supervisor that restarts them.
- **Library first.** Every layer is a reusable Go package. The binary is
  only a composition root.

## How it works

```
   configuration (config.toml)
            |
            v
   +-----------------------------------+
   |            dnsmon run             |
   +-----------------------------------+
            |
            v
   +-----------------------------------+   events    +-------------------+
   |  dns-poller (agent)              | -----------> | change-notifier   |
   |  resolves the records, detects   |              | (agent)           |
   |  the differences                 |              +-------------------+
   +-----------------------------------+                     |
            |                                                  | messages
            v                                                  v
   +-----------------------------------+              +-------------------+
   |  SQLite through GORM              |              | Telegram bot      |
   |  records, snapshots, changes,     |              +-------------------+
   |  tokens, subscribers              |                      ^
   +-----------------------------------+                      |
                                                        +-----+------+
                                                        | telegram- |
                                                        | pairer    |
                                                        | (agent)   |
                                                        +------------+
```

1. The **dns-poller** resolves every monitored record on a fixed interval and
   compares the answer with the previous one.
2. A difference becomes a **change**, stored in SQLite and published on the
   event bus.
3. The **change-notifier** picks the event up and sends the report to every
   paired account.
4. The **telegram-pairer** listens to the commands sent to the bot and
   redeems the pairing tokens.

The first observation of a record only establishes the reference the
following observations are compared to, so it is silent. Set
`monitor.notify_on_baseline = true` to report it as well.

## Getting started

### 1. Create the bot

Talk to [@BotFather](https://t.me/BotFather), create a bot and keep the
token it gives you. The token is the only secret of the monitor. It can be
written in the configuration file or supplied through the environment
variable `DNSMON_TELEGRAM_TOKEN`, which is the safer choice.

### 2. Write the configuration

```bash
cp config.example.toml config.toml
$EDITOR config.toml
```

```toml
[database]
path = "data/dnsmon.db"

[telegram]
token = "123456789:replace-me-with-the-token-of-your-bot"
notify_usernames = ["alice", "bob"]

[monitor]
interval_seconds = 60
resolvers = ["1.1.1.1:53", "8.8.8.8:53"]

[[monitor.records]]
name = "example.com"
type = "A"
```

The records declared in the file are reconciled with the database at every
start: a record added to the file starts being watched and a record removed
from it stops being watched. The records added with `dnsmon records add` are
kept untouched.

### 3. Choose the records to watch

There are two ways to add a record, and they behave differently.

**In the configuration file**, which is the declarative way and the one to
prefer:

```toml
[monitor]
interval_seconds = 60
resolvers = ["1.1.1.1:53", "8.8.8.8:53"]

[[monitor.records]]
name = "example.com"
type = "A"

[[monitor.records]]
name = "example.com"
type = "AAAA"

[[monitor.records]]
name = "example.com"
type = "TXT"
resolver = "9.9.9.9:53"   # optional, overrides the default server
```

The supported types are `A`, `AAAA`, `CNAME`, `TXT`, `NS`, `MX`, `PTR` and
`SOA`. A record without a `resolver` uses the first entry of
`monitor.resolvers`; when that list is empty the lookup fails with
`no DNS server configured for <name>`. Only the first entry is used, the
others are not tried as a fallback.

The file is read when the daemon starts, so a change to it needs a restart
of the daemon to take effect.

**From the command line**, which is the quick way:

```bash
$ dnsmon records add example.com A
the record example.com A is now monitored

$ dnsmon records add example.com AAAA --resolver 9.9.9.9:53
the record example.com AAAA is now monitored

$ dnsmon records list
NAME                                     TYPE     RESOLVER               STATE      SOURCE
example.com                             A        1.1.1.1:53             active     manual

$ dnsmon records remove example.com A
the record example.com A is no longer monitored
```

The type may be given positionally or with `--type`, and it defaults to `A`.
These records are read by the next sweep, so they take effect without
restarting the daemon.

Both ways end up in the same table, `dns_targets`. The `SOURCE` column of
`dnsmon records list` tells which is which: `config` for the records coming
from the file, `manual` for the ones added from the command line. Only the
records coming from the file are reconciled at start, so removing a record
from the file never deletes a record added by hand.

The first observation of a record only establishes the reference the
following observations are compared to, so it is silent. Set
`monitor.notify_on_baseline = true` to report it as well.

A name that carries no record is an observation, not a failure: the monitor
keeps watching it and reports the day the record appears, with the address
it carries, as an addition.

`dnsmon sync` applies the configuration file to the database without
starting the monitor, which is the way to load the records of the file
without waiting for the next start. `dnsmon check` resolves every monitored
record once and shows the answer it gets, compared with the last
observation, without recording anything.

```bash
$ dnsmon sync
6 monitored record(s) and 0 subscriber(s) in data/dnsmon.db
  *.devteam.omnicanal.pe                   A      active   config
  bots.omniatel.omnicanal.pe               A      active   config
  ...

$ dnsmon check
*.devteam.omnicanal.pe                   A      190.105.240.58 (first observation)
bots.omniatel.omnicanal.pe               A      (no answer) (first observation)
```

### 4. Pair the accounts

Only the accounts listed in `telegram.notify_usernames` may use the bot, and
they must present a pairing token before they receive anything.

```bash
$ dnsmon token create
Pairing token: 6fcc0c76-d216-4c58-a19f-72375bf90452
Valid until  : 2026-11-18 16:11:39-05:00
Send to the bot: /start 6fcc0c76-d216-4c58-a19f-72375bf90452
```

The account then sends the command to the bot:

```
/start 6fcc0c76-d216-4c58-a19f-72375bf90452
```

and the bot answers that the account is authorized. A token can only be
redeemed once, expires after `pairing.token_ttl_minutes` minutes, and is not
consumed when the account is not allowed to use the bot.

### 5. Run the monitor

```bash
$ dnsmon run                 # in the foreground
$ dnsmon run --daemon        # in the background
$ dnsmon stop                # stop the background daemon
```

## Commands

| Command | Description |
| --- | --- |
| `dnsmon run [--daemon]` | Run the monitor, in the foreground or as a background daemon |
| `dnsmon stop` | Stop the background daemon |
| `dnsmon status` | Show the monitored records, the paired accounts and the recent changes |
| `dnsmon sync` | Apply the configuration file to the database without starting the monitor |
| `dnsmon check` | Resolve the monitored records once and show the answers |
| `dnsmon token create [--ttl <minutes>]` | Mint a pairing token |
| `dnsmon token list` | List the pairing tokens and their state |
| `dnsmon records list` | List the monitored records |
| `dnsmon records add <name> [type] [--resolver <host:port>]` | Watch a record |
| `dnsmon records remove <name> [type]` | Stop watching a record |
| `dnsmon subscribers list` | List the accounts allowed to receive the reports |
| `dnsmon subscribers remove <username>` | Remove an account |
| `dnsmon version` | Print the version of the binary |

The path of the configuration file is given with `--config`, before or after
the command.

## Configuration reference

| Section | Key | Default | Description |
| --- | --- | --- | --- |
| `database` | `path` | `data/dnsmon.db` | Location of the SQLite file |
| `telegram` | `token` | - | Token of the bot, required |
| `telegram` | `notify_usernames` | `[]` | Accounts allowed to pair and to be notified |
| `telegram` | `poll_timeout_seconds` | `30` | How long a long poll waits for updates |
| `monitor` | `interval_seconds` | `60` | Delay between two checks |
| `monitor` | `resolvers` | `[]` | Name server used when a record names none, only the first entry is used |
| `monitor` | `timeout_seconds` | `5` | Timeout of a single query |
| `monitor` | `notify_on_baseline` | `false` | Report the first observation of a record |
| `monitor.records` | `name` | - | Name of the record to watch |
| `monitor.records` | `type` | - | `A`, `AAAA`, `CNAME`, `TXT`, `NS`, `MX`, `PTR` or `SOA` |
| `monitor.records` | `resolver` | - | Name server used for this record |
| `monitor.records` | `enabled` | `true` | Watch the record without removing it from the file |
| `pairing` | `token_ttl_minutes` | `30` | Lifetime of a pairing token |
| `daemon` | `pid_file` | next to the database | Where the process identifier is written |
| `daemon` | `log_file` | next to the database | Where the background daemon writes its output |
| `log` | `level` | `info` | `debug`, `info`, `warn` or `error` |
| `log` | `format` | `text` | `text` or `json` |

## The libraries

Every package below is usable on its own.

| Package | Role |
| --- | --- |
| `pkg/domain/record` | The watched records, the snapshots and the resolution port |
| `pkg/domain/change` | The detected differences and the change events |
| `pkg/domain/subscriber` | The accounts entitled to receive the reports |
| `pkg/domain/pairing` | The one-shot authorization tokens |
| `pkg/domain/notification` | The outbound messages and the delivery port |
| `pkg/domain/telegram` | The incoming commands and the listening port |
| `pkg/domain/shared` | Clock, identifiers, events and the domain errors |
| `pkg/app/polling` | Use case resolving the records and detecting the changes |
| `pkg/app/notification` | Use case reporting the changes |
| `pkg/app/pairing` | Use case redeeming the pairing tokens |
| `pkg/agent` | The agent contract and the supervisor that keeps them alive |
| `pkg/agent/poller` | The agent watching the records |
| `pkg/agent/notifier` | The agent delivering the reports |
| `pkg/agent/pairer` | The agent authorizing the accounts |
| `pkg/infra/persistence` | The SQLite storage written with GORM |
| `pkg/infra/dns` | The resolver talking to the name servers |
| `pkg/infra/telegram` | The gateway and the notifier of the Telegram bot |
| `pkg/infra/bus` | The in-process event bus |
| `pkg/infra/daemon` | The background operation and the process identifier file |
| `pkg/config` | The loader and the validation of the configuration |
| `pkg/app` | The composition root assembling everything |

The domain packages depend on nothing but the standard library. The
infrastructure packages implement the ports declared by the domain, and the
application layer only knows those ports. The agents are the only place
where the use cases are driven.

## Storage

Everything is kept in a single SQLite file, with the following tables:

| Table | Content |
| --- | --- |
| `dns_targets` | The watched records and their state |
| `dns_snapshots` | The last answer of every record |
| `dns_changes` | The detected differences and the moment they were reported |
| `subscribers` | The accounts allowed to receive the reports |
| `pairing_tokens` | The authorization tokens and their state |

## Development

```bash
make build      # compile the binary into bin/
make test       # run the test suite
make test-race  # run the test suite with the race detector
make lint       # check the formatting and run the compiler checks
make cover      # report the coverage of the test suite
make ci         # run everything the continuous integration runs
make install    # install the binary into ~/.local/bin
make help       # list the available targets
```

The version of the binary comes from git. `make build` stamps the tag, the
commit and the build date into the binary, and `dnsmon version` prints them.

The targets that talk to the daemon take the configuration file from
`CONFIG`, which defaults to `config.toml`:

```bash
make run CONFIG=/etc/dnsmon/config.toml
make status
make stop
```

The test suite covers the domain rules, the use cases, the storage, the
resolver against a name server started by the test, the daemonization and
the whole path from a pairing token to a delivered report.

## License

MIT License - Copyright (c) 2026 Antonio Ognio

Made with ❤️ from 🇵🇪. El Perú es clave 🔑.
