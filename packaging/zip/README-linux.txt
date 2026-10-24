SenHub Agent for Linux
======================

This archive holds the senhub-agent binary. Full documentation:
https://agent.senhub.io/docs/installation/

On Debian, Ubuntu, RHEL and SUSE, prefer the .deb or .rpm package of the
same release, or the apt / dnf / zypper repository at
https://packages.senhub.io/. A machine installed from a package is
upgraded by its package manager, never from this archive.


Verify the download
-------------------

    minisign -Vm senhub-agent-linux-amd64.zip -P RWRlfkyeLpjI0MjTSfuvT/bDNHHaVJhRirQN8Z8LTAM+n4LKVbpjrlRh

Or compare with the SHA256SUMS file of the release:

    sha256sum -c --ignore-missing SHA256SUMS


New install from this archive
-----------------------------

    unzip senhub-agent-linux-amd64.zip -d /tmp/senhub
    sudo /tmp/senhub/senhub-agent install
    sudo /usr/local/bin/senhub-agent start
    sudo /usr/local/bin/senhub-agent status

`install` copies the binary to /usr/local/bin/senhub-agent and runs the
service from there as the unprivileged `senhub` user. The extracted copy
can be deleted afterwards.


Upgrade an agent installed from an archive
------------------------------------------

With access to the release server:

    sudo /usr/local/bin/senhub-agent update <version>

Without it, replace the binary by hand:

    sudo /usr/local/bin/senhub-agent stop
    sudo install -m 0755 -o root -g root ./senhub-agent /usr/local/bin/senhub-agent
    sudo /usr/local/bin/senhub-agent start
    sudo /usr/local/bin/senhub-agent version

The configuration is kept.
