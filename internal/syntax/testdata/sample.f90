program hello
  implicit none
  integer :: n
  real :: x
  n = 0
  x = 3.14
  do while (n < 10)
    n = n + 1
  end do
  ! a comment
  write(*,*) 'done'
end program hello
