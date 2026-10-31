module Cache exposing (lookup)

{-| A documented module.

-}

import Dict exposing (Dict)


type alias Entry =
    { key : String, value : Int }


-- a comment with a "quote"
lookup : String -> Dict String Int -> Maybe Int
lookup key entries =
    case Dict.get key entries of
        Just value ->
            value

        Nothing ->
            -1
