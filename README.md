# Hetzner Cloud API DynDns utility

This is a simple utility to update DNS records managed in the Hetzner DNS service.
Supports A and AAAA records and custom services used to retrieve the effective IP address (uses [SeeIP](https://seeip.org) by default).

When executed without any arguments it reads the `dyndns.json` in the current working directory, otherwise the first argument is used as the path to read.

Sample `dyndns.json` (the actual config does not support comments)
```json5
{
  "HetznerApiKey": "<HETZNER_CLOUD_API_KEY>",
  "ZoneName": "example.de",
  "RecordName": "homelab",
  "RecordTTL": 300,
  "AAAA": {
    "Enabled": true,
//    "Source": "https://ipv6.seeip.org"
  },
  "A": {
    "Enabled": false,
//    "Source": "https://ipv4.seeip.org"
  }
}
```
It is recommended to change the file permissions of `dyndns.json` to `0600` to prevent access to the api key to processes running on the host.

Example crontab entry that checks and if needed updates the address every 10 minutes (given that both files are in the `/root` directory):
```cronexp
*/10 * * * * /root/hetzner_dyndns /root/dyndns.json
```