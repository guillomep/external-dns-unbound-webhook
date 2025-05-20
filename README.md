ExternalDNS - Unbound Webhook
----------------------------------

ExternalDNS is a Kubernetes add-on for automatically managing
Domain Name System (DNS) records for Kubernetes services by using different DNS providers.
By default, Kubernetes manages DNS records internally,
but ExternalDNS takes this functionality a step further by delegating the management of DNS records to an external DNS
provider such as this one.
Therefore, the Unbound webhook allows to manage your
Unbound domains inside your kubernetes cluster with [ExternalDNS](https://github.com/kubernetes-sigs/external-dns).

To use ExternalDNS with Unbound, you need to enable remote control on your
Unbound instance. Check out the [Unbound documentation]() for more information.
For detailed technical instructions on how the Unbound webhook is deployed using the Official Helm chart for ExternalDNS,
see [deployment instructions](#kubernetes-deployment).

## Kubernetes Deployment

The Unbound webhook is provided as a regular Open Container Initiative (OCI) image released in
the [GitHub container registry](https://github.com/guillomep/external-dns-unbound-webhook/pkgs/container/external-dns-unbound-webhook).
The deployment can be performed in every way Kubernetes supports.
The following example shows the deployment as
a [sidecar container](https://kubernetes.io/docs/concepts/workloads/pods/#workload-resources-for-managing-pods) in the
ExternalDNS pod using the [Official Helm chart for ExternalDNS](https://github.com/kubernetes-sigs/external-dns/tree/master/charts/external-dns).

⚠️  This webhook requires at least ExternalDNS v0.14.0.

First, create the Unbound secret if certificate authentication is enabled:

```yaml
kubectl create secret generic unbound-certificates --from-file=ca.pem='<PATH_TO_CA_PEM>' --from-file=client.pem='<PATH_TO_CLIENT_PEM>' --from-file=client.key='<PATH_TO_CLIENT_KEY>' -n external-dns
```

### Using the ExternalDNS chart

Skip this if you already have the ExternalDNS repository added:

```shell
helm repo add external-dns https://kubernetes-sigs.github.io/external-dns/
```

You can then create the helm values file, for example
`external-dns-unbound-values.yaml`:

```yaml
namespace: external-dns
policy: sync
provider:
  name: webhook
  webhook:
    image:
      repository: ghcr.io/guillomep/external-dns-unbound-webhook
      tag: v0.1.0
    env:
    - name: UNBOUND_HOST
      value: <UNBOUND_HOST>
    - name: UNBOUND_CA_PEM_PATH
      value: /usr/local/etc/unbound/ca.pem
    - name: UNBOUND_CERT_PEM_PATH
      value: /usr/local/etc/unbound/client.pem
    - name: UNBOUND_KEY_PEM_PATH
      value: /usr/local/etc/unbound/client.key
    livenessProbe:
      httpGet:
        path: /health
        port: http-webhook
      initialDelaySeconds: 10
      timeoutSeconds: 5
    readinessProbe:
      httpGet:
        path: /ready
        port: http-webhook
      initialDelaySeconds: 10
      timeoutSeconds: 5
    extraVolumeMounts:
    - name: unbound-certificates
      mountPath: /usr/local/etc/unbound/
extraVolumes:
- name: unbound-certificates
  secret:
    secretName: unbound-certificates

extraArgs:
  - "--txt-prefix=reg-%{record_type}-"
```

Replace `<UNBOUND_HOST>` with the real value (e.g. `tcp://192.168.1.1:8953`).

And then:

```shell
# install external-dns with helm
helm install external-dns-unbound external-dns/external-dns -f external-dns-unbound-values.yaml -n external-dns
```

## Environment variables

The following environment variables are available:

| Variable                | Description                                       | Notes                      |
| ----------------------- | ------------------------------------------------- | -------------------------- |
| UNBOUND_HOST            | Unbound host (with port) to control               | Mandatory                  |
| UNBOUND_CA_PEM_PATH     | Server certificate use by Unbound                 | Default: ``                |
| UNBOUND_CLIENT_PEM_PATH | Client certificate use to authenticate to Unbound | Default: ``                |
| UNBOUND_KEY_PEM_PATH    | Server certificate use to authenticate to Unbound | Default: ``                |
| DRY_RUN                 | If set, changes won't be applied                  | Default: `false`           |
| DEFAULT_TTL             | Default TTL if not specified                      | Default: `7200`            |
| WEBHOOK_HOST            | Webhook hostname or IP address                    | Default: `localhost`       |
| WEBHOOK_PORT            | Webhook port                                      | Default: `8888`            |
| HEALTH_HOST             | Liveness and readiness hostname                   | Default: `0.0.0.0`         |
| HEALTH_PORT             | Liveness and readiness port                       | Default: `8080`            |
| READ_TIMEOUT            | Servers' read timeout in ms                       | Default: `60000`           |
| WRITE_TIMEOUT           | Servers' write timeout in ms                      | Default: `60000`           |

Additional environment variables for domain filtering:

| Environment variable           | Description                        |
| ------------------------------ | ---------------------------------- |
| DOMAIN_FILTER                  | Filtered domains                   |
| EXCLUDE_DOMAIN_FILTER          | Excluded domains                   |
| REGEXP_DOMAIN_FILTER           | Regex for filtered domains         |
| REGEXP_DOMAIN_FILTER_EXCLUSION | Regex for excluded domains         |

If the `REGEXP_DOMAIN_FILTER` is set, the following variables will be used to
build the filter:

 - REGEXP_DOMAIN_FILTER
 - REGEXP_DOMAIN_FILTER_EXCLUSION

 otherwise, the filter will be built using:

 - DOMAIN_FILTER
 - EXCLUDE_DOMAIN_FILTER

## Tweaking the configuration

While tweaking the configuration, there are some points to take into
consideration:

- if `WEBHOOK_HOST` and `HEALTH_HOST` are set to the same address/hostname or
  one of them is set to `0.0.0.0` remember to use different ports.
- if your records don't get deleted when applications are uninstalled, you
  might want to verify the policy in use for ExternalDNS: if it's `upsert-only`
  no deletion will occur. It must be set to `sync` for deletions to be
  processed. Please add the following to `external-dns-unbound-values.yaml` if
  you want this strategy:

  ```yaml
  policy: sync
  ```

## Development

The basic development tasks are provided by make. Run `make help` to see the
available targets.

## Contributing

This work is based on the [Vultr webhook implementation](https://github.com/vultr/external-dns-vultr-webhook/tree/main).

You can help this project by giving time to fill issues or creating pull requests, or if you don't have time you can always buy me a coffee.

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://buymeacoffee.com/guillomep)

*BTC*: bc1q8c0q9u5qczxrmj9wx6ukg7a2cnxhea5xs4rav9

*LTC*: ltc1qdudg5ralpptu7clr0ruklfmr06vlgl8vdzp0fj

