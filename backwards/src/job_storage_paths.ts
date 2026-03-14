/// <reference path="../../packages/pocketci/src/global.d.ts" />

export function zeroPad(num: number, places: number): string {
  return String(num).padStart(places, "0");
}

export function zeroPadWithLength(num: number, length: number): string {
  const decimalPlaces = String(length).split(".")[1]?.length || 0;
  return zeroPad(num, decimalPlaces);
}

export class JobStoragePaths {
  constructor(
    private buildID: string,
    private jobName: string,
  ) {}

  getBaseStorageKey(): string {
    return `/pipeline/${this.buildID}/jobs/${this.jobName}`;
  }

  getStepIdentifier(step: Step): string {
    if ("task" in step) {
      return `tasks/${step.task}`;
    } else if ("get" in step) {
      return `get/${step.get}`;
    } else if ("put" in step) {
      return `put/${step.put}`;
    } else if ("do" in step) {
      return "do";
    } else if ("try" in step) {
      return "try";
    } else if ("in_parallel" in step) {
      return "in_parallel";
    } else if ("notify" in step) {
      const name = Array.isArray(step.notify)
        ? step.notify.join("-")
        : step.notify;
      return `notify/${name}`;
    } else if ("agent" in step) {
      return `agent/${step.agent}`;
    }

    return "unknown";
  }

  generateStorageKeyForStep(step: Step, currentPath: string): string {
    return `${this.getBaseStorageKey()}/${currentPath}/${
      this.getStepIdentifier(step)
    }`;
  }

  withAttemptPath(path: string, attempt?: number): string {
    if (!attempt) {
      return path;
    }

    return `${path}/attempt/${attempt}`;
  }
}
