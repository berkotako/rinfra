package sliver

// DeployWithRunner is exported for tests — it exposes deploySliver with an
// injectable Runner so tests never need a real SSH connection.
var DeployWithRunner = deploySliver
