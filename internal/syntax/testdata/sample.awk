# count lines
BEGIN { total = 0 }
{
    total += NF
    if ($1 ~ /^#/) next
}
END { print total, NR }
