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

  withAttemptPath(path: string, attempt?: number): string {
    if (!attempt) {
      return path;
    }

    return `${path}/attempt/${attempt}`;
  }
}
