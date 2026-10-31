module Sample where

import Data.List (sortBy)

-- A documented function.
describe :: Int -> String
describe n
    | n < 0 = "negative"
    | n == 0 = "zero"
    | otherwise = "positive"

{- a block comment
   over two lines -}
lengths :: [[a]] -> [Int]
lengths xs = map length xs
