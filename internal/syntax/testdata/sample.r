# A bounded cache of recent keys.

Cache <- R6::R6Class(
  "Cache",
  public = list(
    capacity = 16,
    slots = character(0),

    initialize = function(capacity = 16) {
      self$capacity <- capacity
    },

    push = function(key) {
      if (length(self$slots) >= self$capacity) {
        self$slots <- self$slots[-1]
      }
      self$slots <- c(key, self$slots)
      invisible(self)
    },

    contains = function(key) key %in% self$slots
  )
)

# Numbers in several bases are read by strtoi.
mask <- strtoi("ff", base = 16L)
ratio <- 1.5e3
stopifnot(!is.na(mask), ratio > 0)

cache <- Cache$new(capacity = 0x10)
cache$push("one")$push("two")
print(cache$contains("one"))
