"""Exercise the installed s2s consumer against the local Go contract server."""

import asyncio
import sys
from datetime import datetime
from types import SimpleNamespace

import httpx

from abita_s2s.agent import AbitaAgent
from abita_s2s.identity import PatientResolver
from abita_s2s.middleware import PatientMiddleware, Receipt
from abita_s2s.offices import SPRING_HILL
from abita_s2s.scheduling import Scheduling
from abita_s2s.scheduling_http import SchedulingHTTP
from abita_s2s.state import CallContext, CallState


async def main(url, expected_status):
    assert url.startswith("http://127.0.0.1:"), "Local fixture server required"
    now = datetime.fromisoformat("2026-06-01T12:00:00+00:00")
    config = SimpleNamespace(middleware_url=url, middleware_token="test-auth")
    state = CallState(CallContext("contract", now, "abita", SPRING_HILL.key))
    context = SimpleNamespace(
        userdata=state, function_call=SimpleNamespace(call_id="contract")
    )
    async with httpx.AsyncClient() as client:
        resolver = PatientResolver(state, PatientMiddleware(client, config))
        scheduling = Scheduling(state, SchedulingHTTP(client, config), now=lambda: now)
        agent = AbitaAgent(SPRING_HILL, None, resolver, scheduling=scheduling)
        try:
            text = await agent.resolve_patient(context, "Jane", "01/15/1980")
            assert isinstance(state.patient.active, Receipt), text
            assert state.patient.active.appointmentsStatus == expected_status
            assert text.startswith("success:"), text
            if expected_status == "error":
                assert "appointments could not be loaded" in text, text
                assert "retry resolution" in text, text
                assert "No upcoming appointments" not in text, text
                assert not scheduling.appointments(), text
            elif expected_status == "none":
                assert "No upcoming appointments." in text, text
            else:
                assert "Existing appointments:" in text, text
                assert scheduling.appointments(), text
            for private in ("12345", "54321", "cancellationToken", "rescheduleToken"):
                assert private not in text, text
            print(text)

            # Missing appointment history must not block an independently
            # verified new slot; existing-appointment mutations still need proof.
            inventory = await scheduling.availability("medical", "2026-06-03")
            assert inventory["outcome"] == "found", inventory
            args = dict(
                appointmentSlotRef=inventory["slots"][0]["appointmentSlotRef"],
                appointmentReason="Medical follow up",
                referringDoctor="none",
                readBack=True,
            )
            booked = await scheduling.book_appointment(context, **args)
            assert booked.startswith("success:"), booked
            assert await scheduling.book_appointment(context, **args) == booked
            for private in ("98765", "bookingToken", "cancellationToken", "rescheduleToken"):
                assert private not in booked, booked
            print(booked)
        finally:
            await resolver.aclose()
            await scheduling.aclose()


if __name__ == "__main__":
    asyncio.run(main(*sys.argv[1:]))
