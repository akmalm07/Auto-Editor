import { spawn } from "node:child_process";
import { once } from "node:events";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

import type { GoTools, Stage, WorkflowContext } from "./graph.js";

const repositoryRoot = resolve(fileURLToPath(new URL("../..", import.meta.url)));

export interface GoCommand {
  command: string;
  args: readonly string[];
}

export interface GoCLIToolsOptions {
  project: string;
  command?: GoCommand;
}

// GoCLITools is the TypeScript graph's adapter for the deterministic Go CLI.
// It uses spawn without a shell so project paths and arguments remain literal.
export class GoCLITools implements GoTools {
  private readonly command: GoCommand;
  private readonly project: string;

  constructor(options: GoCLIToolsOptions) {
    this.project = resolve(options.project);
    this.command = options.command ?? { command: "go", args: ["run", "./cmd/auto-editor"] };
  }

  async readWorkflowContext(): Promise<WorkflowContext> {
    const output = await this.invoke("workflow-context", ["--json"], true);
    let parsed: unknown;
    try {
      parsed = JSON.parse(output);
    } catch (error) {
      throw new Error(`Go workflow-context returned invalid JSON: ${error instanceof Error ? error.message : String(error)}`);
    }
    if (!isWorkflowContext(parsed)) {
      throw new Error("Go workflow-context returned an invalid workflow context");
    }
    return parsed;
  }

  async runStage(stage: Stage, args: readonly string[]): Promise<void> {
    await this.invoke(stage, args, false);
  }

  private async invoke(subcommand: string, args: readonly string[], captureOutput: boolean): Promise<string> {
    const child = spawn(
      this.command.command,
      [...this.command.args, "--project", this.project, subcommand, ...args],
      { cwd: repositoryRoot, shell: false, stdio: captureOutput ? ["ignore", "pipe", "inherit"] : "inherit" },
    );
    let output = "";
    if (captureOutput) {
      child.stdout?.setEncoding("utf8");
      child.stdout?.on("data", (chunk: string) => {
        output += chunk;
      });
    }
    const [code, signal] = (await once(child, "close")) as [number | null, NodeJS.Signals | null];
    if (code !== 0) {
      const reason = signal ? `signal ${signal}` : `exit code ${code}`;
      throw new Error(`Go tool ${subcommand} failed with ${reason}`);
    }
    return output;
  }
}

function isWorkflowContext(value: unknown): value is WorkflowContext {
  return typeof value === "object" && value !== null && typeof (value as WorkflowContext).hasReferences === "boolean";
}
