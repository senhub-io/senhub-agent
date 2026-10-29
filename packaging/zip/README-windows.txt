SenHub Agent for Windows
========================

This archive holds senhub-agent.exe (the agent) and senhub-console.exe
(the web console launcher). Run every command below from an elevated
PowerShell (Run as Administrator), in the folder holding the two files.

Full documentation: https://agent.senhub.io/docs/installation/


Which package to use
--------------------

On servers and managed fleets, prefer the signed MSI installer published
with the same release: senhub-agent-<version>-amd64.msi. A machine
installed by the MSI is upgraded by the MSI, never from this archive.


Verify the download
-------------------

    minisign -Vm senhub-agent-windows-amd64.zip -P RWRlfkyeLpjI0MjTSfuvT/bDNHHaVJhRirQN8Z8LTAM+n4LKVbpjrlRh

Or compare with the SHA256SUMS file of the release:

    (Get-FileHash .\senhub-agent-windows-amd64.zip -Algorithm SHA256).Hash


New install from this archive
-----------------------------

    mkdir C:\SenHub
    Expand-Archive .\senhub-agent-windows-amd64.zip -DestinationPath C:\SenHub\
    cd C:\SenHub
    .\senhub-agent.exe install
    .\senhub-agent.exe start
    .\senhub-agent.exe status

The configuration is written under C:\ProgramData\SenHub\.


Upgrade an agent installed from an archive
------------------------------------------

With access to the release server:

    .\senhub-agent.exe update <version>

Without it, replace the files by hand in the install folder:

    .\senhub-agent.exe stop
    (copy the new senhub-agent.exe and senhub-console.exe over the old ones)
    .\senhub-agent.exe start
    .\senhub-agent.exe version

The configuration under C:\ProgramData\SenHub\ is kept.


Upgrade an agent installed by the MSI
-------------------------------------

Install the newer MSI over the older one; the configuration is kept:

    msiexec /i senhub-agent-<version>-amd64.msi /qn
