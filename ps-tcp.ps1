param(
  [string]$ServerHost = "localhost",
  [int]$Port = 3000
)

$tcp = [Net.Sockets.TcpClient]::new()
try {
  $tcp.Connect($ServerHost, $Port)
} catch {
  Write-Host "❌ Connect failed: $($_.Exception.Message)"
  return
}

$ns = $tcp.GetStream()
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

Write-Host "Connected to $($ServerHost):$Port. Type 'quit' to exit."

# --- RAW READER: no line parsing; prints whatever the server sends ---
$cts = New-Object Threading.CancellationTokenSource
$readerTask = [Threading.Tasks.Task]::Run([Action]{
  $buf = New-Object byte[] 4096
  try {
    while (-not $cts.IsCancellationRequested) {
      $n = $ns.Read($buf, 0, $buf.Length)     # blocks until bytes arrive
      if ($n -le 0) { break }                 # server closed
      $text = [Text.Encoding]::UTF8.GetString($buf, 0, $n)
      Write-Host "`r`n<< $text"
      Write-Host -NoNewline "> "
    }
  } catch {}
})

# --- WRITER: send raw bytes (no BOM) and always add '\n' ---
while ($true) {
  Write-Host -NoNewline "> "
  $line = [Console]::ReadLine()
  if ($null -eq $line -or $line -eq 'quit') { break }
  $bytes = [Text.Encoding]::UTF8.GetBytes($line + "`n")
  $ns.Write($bytes, 0, $bytes.Length)
}

$cts.Cancel()
$tcp.Close()
try { $readerTask.Wait(300) } catch {}
Write-Host "`nDisconnected."
