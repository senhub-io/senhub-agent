using System;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows.Forms;
using WixToolset.Dtf.WindowsInstaller;

namespace SenHub.Installer
{
    public static class CustomActions
    {
        [DllImport("user32.dll")]
        private static extern IntPtr GetForegroundWindow();

        // Owner for the file dialog, so it opens in front of the wizard
        // rather than behind it.
        private sealed class ForegroundWindow : IWin32Window
        {
            public IntPtr Handle { get; } = GetForegroundWindow();
        }

        // Opens the Windows "Open file" dialog and stores the chosen path in
        // LICENSE_FILE. Runs as an immediate action from the options page's
        // Browse button, in the wizard's own process. Cancelling leaves the
        // property untouched. The dialog needs an STA thread, which a custom
        // action thread is not, so it is shown from a dedicated one.
        [CustomAction]
        public static ActionResult BrowseLicenseFile(Session session)
        {
            string chosen = null;
            string current = session["LICENSE_FILE"];

            var thread = new Thread(() =>
            {
                using (var dialog = new OpenFileDialog())
                {
                    dialog.Title = "Select the SenHub Agent licence file";
                    dialog.Filter = "Licence files (*.jwt)|*.jwt|All files (*.*)|*.*";
                    dialog.CheckFileExists = true;
                    dialog.Multiselect = false;
                    if (!string.IsNullOrEmpty(current))
                    {
                        try { dialog.InitialDirectory = System.IO.Path.GetDirectoryName(current); }
                        catch (ArgumentException) { }
                    }
                    if (dialog.ShowDialog(new ForegroundWindow()) == DialogResult.OK)
                    {
                        chosen = dialog.FileName;
                    }
                }
            });
            thread.SetApartmentState(ApartmentState.STA);
            thread.Start();
            thread.Join();

            if (chosen != null)
            {
                session["LICENSE_FILE"] = chosen;
                session.Log("BrowseLicenseFile: LICENSE_FILE set to a file in " + System.IO.Path.GetDirectoryName(chosen));
            }
            else
            {
                session.Log("BrowseLicenseFile: cancelled, LICENSE_FILE unchanged");
            }
            return ActionResult.Success;
        }
    }
}
