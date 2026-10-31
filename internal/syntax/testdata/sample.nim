# nim sums
proc fib(n: int): int =
  if n < 2: n
  else: fib(n - 1) + fib(n - 2)

echo """
multi
line
"""
let x = r"raw"
#[ a block ]#
echo fib(10)
