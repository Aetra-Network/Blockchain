param(
  [string]$OutputDir = ".localnet",
  [string]$Binary = "build\aetrad.exe",
  [string]$ChainId = "aetra-local-1",
  [int]$RPCPort = 26657,
  [int]$Count = 150,
  [string]$Fees = "600000000naet",
  [string]$Amount = "1000naet"
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$node = "tcp://127.0.0.1:$RPCPort"
$fromHome = Join-Path $OutputDir "node0\aetrad"
$fromAddress = Get-LocalnetKeyAddress -Binary $Binary -NodeHome $fromHome -KeyName "node0"
$toAddress = Get-LocalnetKeyAddress -Binary $Binary -NodeHome (Join-Path $OutputDir "node1\aetrad") -KeyName "node1"

$acct = Invoke-LocalnetCliJson -Binary $Binary -Arguments @("query","auth","account", (& $Binary keys show node0 -a --home $fromHome --keyring-backend test), "--node", $node, "--output", "json")
$accountNumber = [int64]$acct.account.value.account_number
$sequence = [int64]$acct.account.value.sequence
Write-Host "starting sequence=$sequence account_number=$accountNumber"

$workDir = Join-Path $fromHome "tmp-burst"
New-Item -ItemType Directory -Force -Path $workDir | Out-Null
$utf8NoBom = New-Object System.Text.UTF8Encoding $false

for ($i = 0; $i -lt $Count; $i++) {
  $seq = $sequence + $i
  $unsigned = Invoke-LocalnetCliJson -Binary $Binary -Arguments @(
    "tx", "bank", "send", "node0", $toAddress, $Amount,
    "--home", $fromHome,
    "--chain-id", $ChainId,
    "--keyring-backend", "test",
    "--fees", $Fees,
    "--gas", "150000",
    "--generate-only",
    "--output", "json"
  )
  $unsigned = Set-LocalnetBankSendMessageAddresses -UnsignedTx $unsigned -FromAddress $fromAddress -ToAddress $toAddress

  $unsignedPath = Join-Path $workDir "unsigned-$seq.json"
  $signedPath = Join-Path $workDir "signed-$seq.json"
  [System.IO.File]::WriteAllText($unsignedPath, ($unsigned | ConvertTo-Json -Depth 100), $utf8NoBom)

  & $Binary tx sign $unsignedPath `
    --from node0 --home $fromHome --chain-id $ChainId --keyring-backend test `
    --node $node --output json --output-document $signedPath `
    --offline --account-number $accountNumber --sequence $seq 2>$null | Out-Null

  & $Binary tx broadcast $signedPath --broadcast-mode async --node $node --output json 2>$null | Out-Null
  Remove-Item -LiteralPath $unsignedPath, $signedPath -ErrorAction SilentlyContinue
}
Write-Host "submitted $Count async txs, sequence $sequence..$($sequence + $Count - 1)"
