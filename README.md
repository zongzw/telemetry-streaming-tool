# telemetry-streaming-tool

This program(written in Go) helps to `setup` or `teardown` F5 Telemetry Streaming(TS) on F5 BIG-IPs based on the predefined `ts-settings.json`.

The `setup` includes(in sequence if need): 

* Verify BIG-IP connection and TS installation status.
* Upload package of specified version.
* Install f5-telemetry-streaming after uploading. 
* Check the uploading and installing done with success.
* Deploy the specifiedtelemetry streaming declaration.

The `teardown` includes:

* uninstall TS package from BIG-IP.

The program can run any times(**idempotent**). It's **declarative** mode. 

The execution processes for different BIG-IPs run **cocurrently**(see `-n`).
## Usage:

```
$ ./telemetry-streaming-tool -h
Setting up Telemetry Streaming on BIG-IP ...
Usage of ./telemetry-streaming-tool:
  -d    Uninstall for all targets.
  -n int
        Cocurrency count for executions. (default 3)
```

### Example

```
./telemetry-streaming-tool -n 5
```
The program outputs logs to `stdout`:

```
2020/06/10 21:53:52 [INFO] Target 10.145.66.217: TS version matched 1.12.0, skip.
2020/06/10 21:53:52 [INFO] Target 10.145.66.217: TS info: {"nodeVersion":"v4.8.0","version":"1.12.0","release":"3","schemaCurrent":"1.12.0","schemaMinimum":"0.9.0"}
2020/06/10 21:53:53 [INFO] Target 10.145.66.217: deployed template successfully
2020/06/10 21:53:54 [INFO] Target 10.145.74.69: TS info: {"nodeVersion":"v4.8.0","version":"1.12.0","release":"3","schemaCurrent":"1.12.0","schemaMinimum":"0.9.0"}
```

And what's more, print the execution summary:

```

Running Summary

10.145.69.178      : [verify: y upload: x]
10.145.69.238      : [verify: y upload: y install: y check: y deploy: y]
10.145.66.217      : [verify: y upload: y install: y check: y deploy: y]
10.145.74.69       : [verify: y upload: y install: y check: y deploy: y]
10.145.74.78       : [verify: y upload: y install: y check: y deploy: y]
10.145.74.55       : [verify: y upload: y install: y check: y deploy: y]
10.145.74.95       : [verify: y upload: y install: y check: y deploy: y]
```

Here:

* `x` means failed and no sequent execution any more.
* `y` means done of this execution.
* `-` means the execution is skipped.

Check the log for failure of that execution.
Run `./telemetry-streaming-tool` again may fix failures caused by occasional unstable situations.

```

Running Summary

10.145.74.95       : [verify: y upload: - install: - check: y deploy: y]
10.145.69.238      : [verify: y upload: - install: - check: y deploy: y]
10.145.69.178      : [verify: y upload: y install: y check: y deploy: y]
10.145.66.217      : [verify: y upload: - install: - check: y deploy: y]
10.145.74.69       : [verify: y upload: - install: - check: y deploy: y]
10.145.74.78       : [verify: y upload: - install: - check: y deploy: y]
10.145.74.55       : [verify: y upload: - install: - check: y deploy: y]
```
Here, only `10.145.69.178` is performed `upload` and `install` which fails  last time. 
`check` and `deploy` will be always performed.

## ts-settings.json

`telemetry-streaming-tool` reads the json file and parse `schedules`. 

A `schedule` uses specific `package`(referred as `version`) and `template` as defined.

A `template` is the standard TS declaration.

A `package` is TS package info for `upload` and `install`.
