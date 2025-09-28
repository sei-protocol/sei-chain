from nova.logic.risk import RiskEngine


def test_split_allocation_even_distribution():
    engine = RiskEngine(buffer=1000)
    allocation = engine.split_allocation(9000, ["val1", "val2", "val3"])
    assert allocation == [("val1", 3000), ("val2", 3000), ("val3", 3000)]


def test_within_limits_respects_buffer():
    engine = RiskEngine(buffer=5000)
    assert not engine.within_limits(4000)
    assert engine.within_limits(6000)
