import { BrandForm } from "../../config/BrandForm";
import { BrandTable } from "../../config/BrandTable";
import { Credentials as CredentialList } from "../../config/Credentials";
import { QuestionEditor } from "../../config/QuestionEditor";
import { RunSettingsForm } from "../../config/RunSettings";
import { ScheduleStatus } from "../../config/ScheduleStatus";
import { WherePeopleAsk as Surfaces } from "../../config/WherePeopleAsk";
import { SettingsHeader } from "./Settings";

// Settings pages over the same components onboarding uses, so each setup screen exists once.

export function General() {
  return (
    <>
      <SettingsHeader title="General">Your brand, how answers are matched to it, the market engines ask from, and how long raw responses are kept.</SettingsHeader>
      <BrandForm retention />
    </>
  );
}

export function Competitors() {
  return (
    <>
      <SettingsHeader title="Competitors">
        Every brand here that is not you and not a technology counts in share of voice. Changes apply from the next check.
      </SettingsHeader>
      <BrandTable />
    </>
  );
}

export function Questions() {
  return (
    <>
      <SettingsHeader title="Questions">
        The questions every check asks, sent exactly as written. Saving a change makes a new version, and you say which new question carries on each old one so its history
        continues.
      </SettingsHeader>
      <QuestionEditor history />
    </>
  );
}

export function WherePeopleAsk() {
  return (
    <>
      <SettingsHeader title="Where people ask">
        The surfaces each check asks, with the models for developer tools and stand-ins. Changing a route or model starts a new series.
      </SettingsHeader>
      <Surfaces />
    </>
  );
}

export function Schedule() {
  return (
    <>
      <SettingsHeader title="Schedule">One setting for every surface and question. Saving works out the next check again right away.</SettingsHeader>
      <ScheduleStatus />
      <RunSettingsForm schedule />
    </>
  );
}

export function Limits() {
  return (
    <>
      <SettingsHeader title="Limits">What a check may spend, and how hard it calls providers.</SettingsHeader>
      <RunSettingsForm limits />
    </>
  );
}

export function Credentials() {
  return (
    <>
      <SettingsHeader title="Credentials">
        The Treg token and your own provider keys. They are stored encrypted, sent only to their provider, and never shown again after you save them.
      </SettingsHeader>
      <CredentialList />
    </>
  );
}
