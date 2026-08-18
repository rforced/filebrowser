#!/usr/bin/env python3
"""Regenerate the HDF5 fixtures.

Requires h5py. The fixtures mimic the shape of CONVERGE output without
containing any customer data:

    post.h5     a miniature post*.h5 — boundaries, CSR connectivity with the
                negative-owner encoding, cell data, and a parcel branch
    restart.h5  the root attributes a .rst carries
    odd.h5      types the reader must reject or handle at the edges

libver="earliest" is what makes these match CONVERGE: it forces superblock v0
and old-style symbol-table groups, which is the dialect the reader implements.
Compression is deliberately off — CONVERGE writes contiguous, unfiltered data.

    python3 generate.py
"""

import h5py
import numpy as np

HERE = __file__.rsplit("/", 1)[0]


def post():
    with h5py.File(f"{HERE}/post.h5", "w", libver="earliest") as f:
        for k, v in {
            "OUTPUT_TIME": -359.94486439338516,
            "CRANK_ANGLE": -359.94486439338516,
            "OUTPUT_TIME_SEC": -0.019996936910743623,
            "CRANK_FLAG": 1,
            "RPM": 3000.0,
            "VERSION_FLAG": 4,
            "VERSION_NUM1": 6,
            "VERSION_NUM2": 0,
            "VERSION_NUM3": 1,
        }.items():
            f.attrs.create(k, np.array([v]))

        b = f.create_group("BOUNDARIES")
        b.create_dataset("BOUNDARY_IDS", data=np.array([1, 2, 3], np.int32))
        b.create_dataset(
            "BOUNDARY_NAMES", data=np.array([b"PISTON", b"HEAD", b"SPARK PLUG"], "S21")
        )
        b.create_dataset("NUM_ELEMENTS", data=np.array([2, 3, 0], np.int32))
        b.create_dataset("NUM_POINTS", data=np.array([6, 8, 0], np.int32))
        b.create_dataset("STREAMS", data=np.array([b"Stream_00"] * 3, "S10"))
        for axis in "XYZ":
            b.create_dataset(
                f"GEOMETRIC_CENTER_COORDINATE_{axis}", data=np.zeros(3, np.float64)
            )

        s = f.create_group("STREAM_00")
        s.attrs.create("CELL_COUNT", np.array([4]))
        s.attrs.create("OUTPUT_TIME", np.array([-359.94486439338516]))
        s.attrs.create("CRANK_FLAG", np.array([1]))

        v = s.create_group("VERTEX_COORDINATES")
        for axis, vals in zip("XYZ", np.eye(3, 6, dtype=np.float32)):
            v.create_dataset(axis, data=np.asarray(vals, np.float32))

        # 5 faces: two on boundary 1 (owner -2), three on boundary 2 (owner -3).
        # Matches CONVERGE's -(id+1) encoding so the extractor can be tested.
        c = s.create_group("CONNECTIVITY")
        c.create_dataset(
            "POLYGON_OFFSET", data=np.array([0, 3, 7, 10, 13, 17], np.int32)
        )
        c.create_dataset(
            "POLYGON_TO_VERTEX",
            data=np.array(
                [0, 1, 2, 0, 1, 2, 3, 1, 2, 3, 0, 2, 3, 0, 1, 2, 3], np.int32
            ),
        )
        c.create_dataset(
            "CONNECTED_CELLS",
            data=np.array([-2, 0, -2, 1, -3, 2, -3, 3, -3, 0], np.int32),
        )

        d = s.create_group("CELL_CENTER_DATA")
        d.create_dataset("TEMPERATURE", data=np.array([300, 450.5, 1200, 900], np.float32))
        d.create_dataset("PRESSURE", data=np.array([1e5, 1.1e5, 1e5, 1e5], np.float32))
        # A diverged field: one NaN and one Inf for the stats path.
        d.create_dataset(
            "EQUIV_RATIO", data=np.array([0.5, np.nan, np.inf, 1.5], np.float32)
        )

        n = s.create_group("VARIABLE_NAMES")
        n.create_dataset(
            "CELL_VARIABLES",
            data=np.array([b"TEMPERATURE", b"PRESSURE", b"EQUIV_RATIO"], "S16"),
        )
        n.create_dataset(
            "LIQUID_PARCEL_VARIABLES",
            data=np.array([b"RADIUS", b"TEMP", b"PARCEL_X", b"PARCEL_Y", b"PARCEL_Z"], "S11"),
        )

        p = s.create_group("PARCEL_DATA").create_group("LIQUID_PARCEL_DATA")
        p = p.create_group("LIQPARCEL_1")
        p.create_dataset("RADIUS", data=np.array([1e-6, 5e-5, 2e-4], np.float32))
        p.create_dataset("TEMP", data=np.array([300.0, 320.0, 350.0], np.float32))
        p.create_dataset("MASS", data=np.array([1e-9, 2e-9, 3e-9], np.float32))
        for axis, vals in zip(
            "XYZ", [[0.0, 1.0, 2.0], [0.0, 0.5, 1.0], [0.0, -1.0, -2.0]]
        ):
            p.create_dataset(f"PARCEL_{axis}", data=np.array(vals, np.float32))
        for axis in "XYZ":
            p.create_dataset(f"VELOCITY_{axis}", data=np.zeros(3, np.float32))


def restart():
    with h5py.File(f"{HERE}/restart.h5", "w", libver="earliest") as f:
        f.attrs.create("SOLVER_VERSION", np.array([b"CONVERGE 6.0.1"], "S15"))
        f.attrs.create("COMPILE_DATE", np.array([b"Jul 27 2026"], "S12"))
        f.attrs.create("NCYC", np.array([6959], np.int32))
        f.attrs.create("TOTAL_MPI_PROCESSES", np.array([88], np.int32))
        f.attrs.create("RESTART_FILE_NUM", np.array([74], np.int32))
        f.attrs.create("WRITE_COUNT", np.array([23], np.int32))
        f.create_dataset("RANDOM_NUMBER_SEEDS", data=np.arange(4, dtype=np.int32))
        s = f.create_group("STREAM_00")
        s.attrs.create("TIME_STEP", np.array([-120.1942]))
        s.attrs.create("DT", np.array([5.73011577e-06]))
        cc = s.create_group("CELL_CENTER")
        cc.create_dataset("AR", data=np.linspace(0, 1, 8, dtype=np.float64))
        cc.create_dataset("TEMPERATURE", data=np.linspace(300, 900, 8, dtype=np.float64))


def odd():
    with h5py.File(f"{HERE}/odd.h5", "w", libver="earliest") as f:
        f.create_dataset("chunked", data=np.arange(64, dtype=np.float32), chunks=(8,))
        f.create_dataset(
            "compressed", data=np.arange(64, dtype=np.float32), chunks=(8,), compression="gzip"
        )
        f.create_dataset("int8", data=np.arange(4, dtype=np.int8))
        f.create_dataset("uint16", data=np.arange(4, dtype=np.uint16))
        f.create_dataset("int64", data=np.arange(4, dtype=np.int64))
        f.create_dataset("scalar", data=np.float64(42.5))
        f.create_dataset("twod", data=np.arange(6, dtype=np.float32).reshape(2, 3))
        f.create_dataset("empty", data=np.zeros(0, dtype=np.float32))


if __name__ == "__main__":
    post()
    restart()
    odd()
    print("wrote post.h5, restart.h5, odd.h5")
