// Error carrying a machine-readable code that is sent back to the daemon.
export class ToolError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}
