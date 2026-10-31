# Exit with a status code, syscall style.
.intel_syntax noprefix
.text
.globl _start

_start:
    mov rax, 1          # write
    mov rdi, 1          # stdout
    lea rsi, [rip + msg]
    mov rdx, msg_len
    syscall

    mov rax, 60         # exit
    mov rdi, 0x2a
    syscall

msg:
    .asciz "hello, assembly\n"
    .set msg_len, . - msg

; A block of data in several bases.
.data
value:  .quad 0x10
mask:   .byte 0b1111
count:  .long 1000
