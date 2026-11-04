# Frozen Labs research probe (STOP / DO NOT RERUN)

This is a **research instrument, not the bridge or an installation tool**.
For ordinary use, start at the [project README](../../../README.md). For its role
and history, use the [research index](../../../docs/technical/research/README.md).

**Don't rerun it on Labs as a development check.** Research v2 was followed by a
confirmed Labs crash and remains stopped. The separately approved v3 trial
observed a source-first reset with bounded Labs survival and actual **exit 9**,
not a clean-EOF success or approval for another trial.
The [safety summary](../../../docs/technical/QUALIFICATION.md#safety-incident)
retains these distinct outcomes. Complete raw evidence and private trial
ledgers are not part of this source-only public repository.

## Understand the distinct contract

Current mode is `vx-labs-probe/research-only-v3-source-first`. It binds exclusive
loopback 12321, accepts one client and creates the verified native identity window
only after bind. It has no outbound target, no forwarding, no readiness synthesis
and no automatic image-name/vendor configuration changes.

After two distinct ball IDs receive actual local 200 writes, it publishes a
nonfinal/inconclusive goal and keeps the source/window. Receiver-local success
requires later clean EOF at an object boundary plus verified cleanup. Source
reset remains inconclusive; the persistent bridge's reset policy is not assigned
to this diagnostic.

Bounds remain **1 MiB total receive**, depth 64, **295-second network** and
**300-second process watchdog**, with finite writes. Errors/deadlines can still
force teardown beneath an attached source, so the probe isn't a safe substitute
for the bridge. The probe accepts one client only, closes its listener after
accept, rejects malformed/truncated/duplicate-key input without resynchronizing,
and never converts a reset or truncated frame into clean EOF. Writes are capped
at two seconds and by the absolute network deadline. The watchdog can leave an
incomplete summary and cannot guarantee safe source departure.

The native identity is an actual same-process, owner-thread, visible unowned
top-level `ProTee United VX` window, offscreen and transparent/nonactivating,
verified before use. Failure or cleanup errors are explicit, not a hidden-window
fallback. This requirement does not prove that the window caused source
acceptance or that teardown is safe.

Its architectural references are pinned VX Connector v1.5.6
[`GspConnectStub.cs`](https://github.com/zonkey-acoustic/vx-connector/blob/df0f6e8ae359b81a428c6077bf27cbbf6bb037b1/dotnet/GspConnectStub.cs)
and [`OpenConnectListener.cs`](https://github.com/zonkey-acoustic/vx-connector/blob/df0f6e8ae359b81a428c6077bf27cbbf6bb037b1/dotnet/OpenConnectListener.cs).
No upstream source was copied or mechanically translated. See the retained
[notices](THIRD_PARTY_NOTICES.txt).

## Check source without running the executable

Use the root module and pinned compiler in [.go-version](../../../.go-version).
From the repository root, run invented/private fixtures:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go test -mod=readonly -count=1 -timeout=90s ./tools/research/labs-probe
GOTOOLCHAIN=local CGO_ENABLED=0 go vet -mod=readonly ./tools/research/labs-probe
```

To cross-build only, choose a reviewed absolute path outside the checkout:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -mod=readonly -trimpath -buildvcs=true \
  -ldflags="-X main.buildRevision=$(git rev-parse HEAD)" \
  -o /absolute/path/to/artifacts/vx-labs-probe-source-first-v3.exe ./tools/research/labs-probe
```

Don't use GUI subsystem flags; the research console is intentional. Don't
execute the output. Native Windows fixture checks require a development/hosted
runner, not simulator access. Distribute this tool's
[third-party notices](THIRD_PARTY_NOTICES.txt) alongside any separately approved
research artifact.

Reports are bounded, redacted and local under
`%LOCALAPPDATA%\VxBridgeResearch\probe\`. The three possible immutable records
are `running.json`, `goal.json` and final `summary.json`; a goal isn't final
success or a liveness heartbeat. Raw payloads, metrics and actual identifiers
aren't exported. Reports distinguish goal attainment, finality, source departure,
cleanup and process outcome; readers must not turn a nonfinal goal, stale
running record or missing summary into success. Actual exit 9 remains a
nonzero inconclusive result, not a clean-EOF pass. No old trial procedure is
current authorization.
