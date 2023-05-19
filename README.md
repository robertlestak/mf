# mf - mr. freeze

Launch a process which will be frozen if a check process fails, and will be unfrozen when the check process succeeds.

This is useful if your process is dependent on another process which may fail, and you want to make sure that your process is not running while the other process is failing.

## Usage

```
Usage: mf [options] -- <command> [args...]
Options:
  -check string
        check command to run. If the command exits with a non-zero exit code, the process will be frozen until the check command exits with a zero exit code
  -delay string
        delay to run the check command (default "0s")
  -interval string
        interval to run the check command (default "5s")
  -log string
        log level (default "error")
  -pid int
        pid of process to monitor
  -timeout string
        timeout for successive failures of the check command. default is to never timeout (default "0s")
  -version
        print version and exit
```

`mf` can be used in two ways:
- to launch a new process, and monitor it
- to monitor an existing process with the `-pid` option

## Examples

### Launching a new process

```bash
# let's say we have a process which is dependent on our VPN connection
# we want to make sure that our process is not running while the VPN is down
# assuming our vpn is on tun0, we can use "ifconfig tun0" to check if the vpn is up
mf -check 'ifconfig tun0' -- ./my-process
# note the '--' before the command to run is not required, but it is recommended
# to avoid confusion with the options for mf and the options for the command to run
```

Now let's update the check command so that it will automatically reconnect our VPN connection. If / when the connection succeeds, the process will be unfrozen and continue running.

```bash
# first, let's create a script that will check the VPN, if it's down, it will re-start it
cat > vpn-checker.sh <<'EOF'
#!/bin/bash
# Specify the path to your OpenVPN configuration file
OPENVPN_CONFIG="/path/to/your/openvpn.conf"

# Check if connected to VPN
if pgrep openvpn >/dev/null; then
    echo "Connected to VPN."
    exit 0
else
    echo "Not connected to VPN. Reconnecting..."
    sudo openvpn --config "$OPENVPN_CONFIG" --daemon
    # at this point you can either optimistically exit 0,
    # or pessimistically exit 1, and let the next check ensure
    # you could also do a follow up check in here... you get the idea
    exit 1
fi
EOF

# make it executable
chmod +x ./vpn-checker.sh

# run our process with mf, checking every 2 seconds, and exiting if no success after 60s
mf -check './vpn-checker.sh' \
    -interval 2s \
    -timeout 60s \
    ./my-process
```

### Monitoring an existing process

```bash
# let's say we have a process which is dependent on our VPN connection
# which is already running. we can grab the pid of the process
# and use mf to monitor it for us
mf -check 'ifconfig tun0' \
      -pid $(pgrep my-process)
```

