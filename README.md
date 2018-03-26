# kubechange

[![Build Status](https://travis-ci.org/better/kubechange.svg?branch=master)](https://travis-ci.org/better/kubechange)

kubechange helps keep local and remote Kubernetes state up-to-date.

At present, kubechange only works with Job/CronJob resources, but in the future it may be extended to work with Deployments, DaemonSets, Services, etc.

**IMPORTANT:** This is alpha-quality software. Use at your own risk.

## Installation

Download the [latest stable release](https://github.com/better/kubechange/releases) from GitHub.

## Usage

```bash
Usage: kubechange -l <label> <file> ...
kubechange helps keep local and remote Kubernetes state up-to-date

-l string	Label to use as a filter
-e string	Update cluster objects

# Passing files as arguments
kubechange -l common-label -e manifest-foo.yml manifest-bar.yml

# Reading files in stdin
cat manifest.yml | kubechange -l common-label -e -
```

### Common label

In order for kubechange to work, local and remote Kubernetes resources must have a shared label. For example, Deployments might use the `app` label. This shared label is how kubechange finds pairs of resources to compare. This label can be provided with the `-l` flag.

### Dry run

By default, kubechange does a dry run. You have to add the `-e` flag to make changes to remote resources.

### Jobs/CronJobs

kubechange can convert Jobs to CronJobs and vice versa, as long as they have a shared label. In either case, it will delete the remote resource being replaced (automatically deleting child resources) and create the replacing resource.

### Multiple manifests

kubechange accepts multiple manifests in a file (or stdin) and will parse each as a separate resource.

## License

[MIT](https://opensource.org/licenses/MIT)

## Credits

Ivan Malopinsky at [Better Mortgage](https://better.com)
