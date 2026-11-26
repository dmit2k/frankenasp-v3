.syntax unified
.arm
.text
.global _start
.type _start,%function
_start:
 mov r0,sp
 b entry_c
.section .note.GNU-stack,"",%progbits
