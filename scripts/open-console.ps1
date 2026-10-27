param([int]$LocalPort = 18120, [switch]$ShowPassword)
$ErrorActionPreference = 'Stop'
if ($ShowPassword) {
    ssh itemkeep-server "python3 /opt/gpt-ptmove/scripts/show_admin_password.py"
    exit $LASTEXITCODE
}
Write-Host "PTMove console: http://127.0.0.1:$LocalPort"
Write-Host 'Keep this terminal open while using the console. Ctrl+C closes the SSH tunnel.'
ssh -N -o ExitOnForwardFailure=yes -L "${LocalPort}:127.0.0.1:18120" itemkeep-server
