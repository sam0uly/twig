" vim settings
set number
if has("syntax")
  syntax on
endif
function! Greet(name)
  return "Hello, " . a:name
endfunction
