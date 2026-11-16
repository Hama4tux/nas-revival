# Workstation tools

No tools are installed by the bootstrap. Future cross compilers, dtc, image inspection and emulators must be versioned and introduced only when needed. Larger host tools do not belong in the target runtime.

Private captures and NAND backups belong in restricted storage outside public history. Do not import the parent workspace's legacy HTTPS proxy into the product.

The local HamaProject portfolio change is prepared in `hamaproject.patch`; it contains only project-development information. Applying/deploying it requires write access outside the current sandbox. It is an integration handoff, not firmware tooling.
