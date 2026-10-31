-- purescript module
module Main where

import Prelude

fact :: Int -> Int
fact n = if n < 2 then 1 else n * fact (n - 1)

{- block {- nested -} -}

main = log (show (fact 10))
