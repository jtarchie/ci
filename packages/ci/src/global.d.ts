declare global {
  interface Task {
    name: string;
    image: string;
    command: string[];
  }

  interface TaskResult {
    stdout: string;
    stderr: string;
    error: string;
    code: number;
  }

  function run(task: Task): TaskResult;

  namespace assert {
    function containsElement(
      element: any,
      array: any[],
      message?: string,
    ): void;
    function containsString(
      substr: string,
      str: string,
      message?: string,
    ): void;
    function equal(expected: any, actual: any, message?: string): void;
    function notEqual(expected: any, actual: any, message?: string): void;
    function truthy(value: any, message?: string): void;
  }
}

export {};
