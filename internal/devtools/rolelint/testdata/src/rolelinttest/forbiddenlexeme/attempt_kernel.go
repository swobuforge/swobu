package forbiddenlexeme // want `weak file basename "attempt_kernel.go"`

type AttemptKernelSpec struct{} // want `identifier "AttemptKernelSpec" contains forbidden lexeme "kernel"; rename using domain language`

func buildKernel() {} // want `identifier "buildKernel" contains forbidden lexeme "kernel"; rename using domain language`
