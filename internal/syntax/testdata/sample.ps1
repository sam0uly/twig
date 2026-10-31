# A bounded cache of recent keys.
[CmdletBinding()]
param(
    [int]$Capacity = 16
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

class Cache {
    [int]$Capacity
    [System.Collections.Generic.List[string]]$Slots

    Cache([int]$capacity) {
        $this.Capacity = $capacity
        $this.Slots = [System.Collections.Generic.List[string]]::new()
    }

    [void] Push([string]$key) {
        if ($this.Slots.Count -ge $this.Capacity) {
            $this.Slots.RemoveAt(0)
        }
        $this.Slots.Add($key)
    }
}

$cache = [Cache]::new($Capacity)
Get-ChildItem -Path . -Filter *.go | ForEach-Object {
    $cache.Push($_.Name)
}

Write-Host "cached $($cache.Slots.Count) files"

<#
  Numbers in several bases: 0xff, 0b1111, 1.5e3.
  A verbatim string keeps its backslashes: 'C:\temp'.
#>
