' The wizard's Browse button. Windows Installer has no file picker of its
' own, and a dialog opened by the installer's process is created behind
' the wizard: Windows only lets a window come to the front when it belongs
' to the foreground process or to a process it started. So the picker
' runs in a child PowerShell, which shows the standard Open File dialog on
' top of the wizard and hands the chosen path back through a temp file.
Function BrowseLicenseFile()
  On Error Resume Next
  Dim wsh, fso, tmp, ps, cmd, f, chosen
  Set wsh = CreateObject("WScript.Shell")
  Set fso = CreateObject("Scripting.FileSystemObject")
  tmp = fso.GetSpecialFolder(2) & "\senhub-licence-pick.txt"
  If fso.FileExists(tmp) Then fso.DeleteFile tmp
  ps = "Add-Type -AssemblyName System.Windows.Forms;" & _
       "$o=New-Object System.Windows.Forms.Form;$o.TopMost=$true;$o.ShowInTaskbar=$false;$o.Opacity=0;$o.Show();$o.Activate();" & _
       "$d=New-Object System.Windows.Forms.OpenFileDialog;$d.Title='Select the SenHub Agent licence file';" & _
       "$d.Filter='Licence files (*.jwt)|*.jwt|All files (*.*)|*.*';$d.CheckFileExists=$true;$d.Multiselect=$false;" & _
       "if($d.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK){[IO.File]::WriteAllText('" & tmp & "',$d.FileName)};$o.Close()"
  cmd = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -STA -WindowStyle Hidden -Command """ & ps & """"
  wsh.Run cmd, 0, True
  If fso.FileExists(tmp) Then
    Set f = fso.OpenTextFile(tmp, 1)
    chosen = Trim(f.ReadAll)
    f.Close
    fso.DeleteFile tmp
    If Len(chosen) > 0 Then Session.Property("LICENSE_FILE") = chosen
  End If
  BrowseLicenseFile = 1
End Function
