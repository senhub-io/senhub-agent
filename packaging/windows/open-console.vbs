' Opens the web console without a command window: the agent is a console
' program, so a shortcut aimed at it flashes a terminal. This launcher runs
' `senhub-agent console` hidden; the agent still asks for elevation once
' to read the sealed key, then opens the browser as the user.
Dim sh, exe
Set sh = CreateObject("WScript.Shell")
exe = Replace(WScript.ScriptFullName, WScript.ScriptName, "senhub-agent.exe")
sh.Run """" & exe & """ console", 0, False
