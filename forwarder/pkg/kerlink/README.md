# Kerlink Configuration

This folder contains the build scripts for creating kerlink IPK packages.

## Building

To build the package you must also download the ["Remove IPK Package"](https://wikikerlink.fr/wirnet-productline/doku.php?id=wiki:resources:resources#tools) tool and extract it in the `ipk` folder.

You must also first build the `kudzu-forwarder-arm7` binary from the parent package folder.

Then you can build the packages with `make all`