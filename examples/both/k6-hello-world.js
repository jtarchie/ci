// @ts-nocheck - k6 script, not a Deno module
import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  const res = http.get("https://httpbin.org/get");
  check(res, {
    "status is 200": (r) => r.status === 200,
    "body contains url": (r) => r.json("url") !== undefined,
  });
}
