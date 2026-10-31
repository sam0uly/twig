@echo off
:: a comment
set name=world
if "%name%"=="world" (
    echo hello %name%
) else (
    echo nope
)
